package agentmcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/sftpclient"
	"github.com/cmstar/jumpaccess/internal/sshhostkey"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"github.com/cmstar/jumpaccess/internal/target"
	"golang.org/x/crypto/ssh"
)

type IdentitySource interface {
	Identity(context.Context, string) (auth.LoginIdentity, error)
	EnsureFresh(context.Context, string) (credential.Token, error)
}

// Connector 读取默认选择并将唯一目标固定为 ID；不修改配置，不发起交互登录或主机信任弹窗。
type Connector struct {
	Config     ConfigReader
	Auth       IdentitySource
	Queries    *Queries
	Connect    connect.Service
	KnownHosts string
	Timeout    time.Duration
}

func (c Connector) Resolve(ctx context.Context, in Target) (ResolvedTarget, error) {
	if err := validateTargetForm(in); err != nil {
		return ResolvedTarget{}, err
	}
	cfg, err := c.Config.Load()
	if err != nil {
		return ResolvedTarget{}, queryError(err)
	}
	if in.Profile == "" {
		in.Profile = cfg.CurrentProfile
	}
	if strings.TrimSpace(in.Profile) == "" {
		return ResolvedTarget{}, &Fault{Code: "PROFILE_REQUIRED", Message: "未设置当前 Profile，请用 list_profiles 选择后显式提供 profile", Resolution: &ResolutionHint{NextTool: "list_profiles"}}
	}
	p, exists := cfg.Profiles[in.Profile]
	if !exists {
		return ResolvedTarget{}, &Fault{Code: "PROFILE_NOT_FOUND", Message: "Profile 不存在，请用 list_profiles 选择有效配置", Resolution: &ResolutionHint{Profile: in.Profile, NextTool: "list_profiles"}}
	}
	identity, err := c.Auth.Identity(ctx, in.Profile)
	if err != nil {
		return ResolvedTarget{}, queryError(err)
	}
	if in.Name != "" || in.Alias != "" {
		in, err = c.resolveNamedTarget(ctx, cfg, in)
		if err != nil {
			return ResolvedTarget{}, err
		}
	}
	if !identifier.MatchString(in.Organization) || !uuid.MatchString(in.AssetID) || !identifier.MatchString(in.AccountID) || strings.HasPrefix(in.AccountID, "@") {
		return ResolvedTarget{}, fault("INVALID_ARGUMENT", "需要明确的组织 ID、资产 UUID 和托管账号 ID")
	}
	site, err := config.NormalizeProfileURL(p.URL)
	if err != nil {
		return ResolvedTarget{}, fault("INVALID_ARGUMENT", "Profile 站点无效")
	}
	resolved := ResolvedTarget{Target: in, Site: site, CredentialIdentity: loginIdentity(identity.LoginID)}
	if err := c.Validate(ctx, resolved); err != nil {
		return ResolvedTarget{}, err
	}
	return resolved, nil
}

func loginIdentity(id string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(id))) }

func (c Connector) Validate(ctx context.Context, r ResolvedTarget) error {
	cfg, err := c.Config.Load()
	if err != nil {
		return fault("IDENTITY_CHANGED", "无法确认原 Profile 身份")
	}
	p, ok := cfg.Profiles[r.Profile]
	site, err := config.NormalizeProfileURL(p.URL)
	if !ok || err != nil || site != r.Site {
		return fault("IDENTITY_CHANGED", "Profile 已删除或站点已改变")
	}
	i, err := c.Auth.Identity(ctx, r.Profile)
	if err != nil {
		return queryError(err)
	}
	identitySite, err := config.NormalizeProfileURL(i.Site)
	if err != nil || identitySite != r.Site || i.LoginID == "" || loginIdentity(i.LoginID) != r.CredentialIdentity {
		return fault("IDENTITY_CHANGED", "登录身份已改变，请为新身份打开新连接")
	}
	return nil
}

type frozenConfig struct{ value config.Config }

func (c frozenConfig) Load() (config.Config, error) { return c.value, nil }

type boundTokens struct {
	source IdentitySource
	target ResolvedTarget
}

func (b boundTokens) EnsureFresh(ctx context.Context, profile string) (credential.Token, error) {
	t, err := b.source.EnsureFresh(ctx, profile)
	if err != nil {
		return credential.Token{}, err
	}
	if t.LoginID == "" || loginIdentity(t.LoginID) != b.target.CredentialIdentity || auth.ValidateTokenSite(t, b.target.Site) != nil {
		return credential.Token{}, fault("IDENTITY_CHANGED", "准备连接期间登录身份已改变")
	}
	return t, nil
}

func (c Connector) Dial(ctx context.Context, r ResolvedTarget) (*ssh.Client, error) {
	return c.dial(ctx, r, false)
}

func (c Connector) DialUpload(ctx context.Context, r ResolvedTarget) (*ssh.Client, error) {
	return c.dial(ctx, r, true)
}

func requireUploadPermission(actions []jumpserver.LabelValue) error {
	if actions == nil {
		return nil
	}
	for _, action := range actions {
		if action.Value == "upload" {
			return nil
		}
	}
	return fault("UPLOAD_NOT_PERMITTED", "目标账号未授权文件上传")
}

func (c Connector) dial(ctx context.Context, r ResolvedTarget, upload bool) (*ssh.Client, error) {
	prepared, err := c.prepareProtocol(ctx, r, "ssh")
	if err != nil {
		return nil, err
	}
	if upload {
		if err = requireUploadPermission(prepared.Account.Actions); err != nil {
			return nil, err
		}
	}
	callback, err := (sshhostkey.Store{Path: c.KnownHosts}).Callback(ctx, false)
	if err != nil {
		return nil, fault("HOST_KEY_UNAVAILABLE", "无法读取主机信任记录")
	}
	client, err := (sshupstream.Dialer{HostKeyCallback: callback, Timeout: c.Timeout}).Dial(ctx, prepared.Connection)
	if err != nil {
		return nil, fault("CONNECT_FAILED", "SSH 建连失败；请检查网络及主机信任，未知主机需先通过人用入口确认")
	}
	return client, nil
}

func (c Connector) prepareProtocol(ctx context.Context, r ResolvedTarget, protocol string) (connect.Prepared, error) {
	if err := c.Validate(ctx, r); err != nil {
		return connect.Prepared{}, err
	}
	// 固定配置副本中不含 Alias，防止恰好同名的 Alias 覆盖已解析的资产 ID。
	cfg := config.Default()
	cfg.Profiles[r.Profile] = config.Profile{URL: r.Site, Organization: r.Organization}
	service := c.Connect
	service.Config = frozenConfig{cfg}
	service.Tokens = boundTokens{c.Auth, r}
	prepared, err := service.Prepare(ctx, connect.Options{Target: target.Input{Profile: r.Profile, Organization: r.Organization, Target: r.AssetID, Account: r.AccountID}, Protocol: protocol, NonInteractive: true})
	if err != nil {
		if errors.Is(err, auth.ErrLoginRequired) {
			return connect.Prepared{}, asFault(queryError(err), "AUTH_REQUIRED", "请重新登录")
		}
		if protocol == "sftp" && errors.Is(err, connect.ErrProtocolNotPermitted) {
			return connect.Prepared{}, fault("SFTP_NOT_PERMITTED", "目标未授权 SFTP 协议")
		}
		return connect.Prepared{}, asFault(err, "CONNECT_FAILED", "无法准备连接，请检查目标协议权限和登录")
	}
	if prepared.Asset.ID != r.AssetID || prepared.Account.ID != r.AccountID {
		return connect.Prepared{}, fault("IDENTITY_CHANGED", "连接准备返回了不同目标或账号")
	}
	if err := c.Validate(ctx, r); err != nil {
		return connect.Prepared{}, err
	}
	return prepared, nil
}

func (c Connector) OpenDownloadSFTP(ctx context.Context, r ResolvedTarget) (DownloadSFTP, error) {
	return c.openFileSFTP(ctx, r, false)
}

func (c Connector) OpenUploadSFTP(ctx context.Context, r ResolvedTarget) (UploadSFTP, error) {
	opened, err := c.openFileSFTP(ctx, r, true)
	if err != nil {
		return UploadSFTP{}, err
	}
	client, ok := opened.Client.(UploadRemote)
	if !ok {
		opened.Client.Close()
		return UploadSFTP{}, fault("SFTP_UNAVAILABLE", "SFTP 上传能力不可用")
	}
	return UploadSFTP{Client: client, Root: opened.Root, Account: opened.Account}, nil
}

func (c Connector) openFileSFTP(ctx context.Context, r ResolvedTarget, upload bool) (DownloadSFTP, error) {
	prepared, err := c.prepareProtocol(ctx, r, "sftp")
	if err != nil {
		return DownloadSFTP{}, err
	}
	if upload {
		if err = requireUploadPermission(prepared.Account.Actions); err != nil {
			return DownloadSFTP{}, err
		}
	}
	callback, err := (sshhostkey.Store{Path: c.KnownHosts}).Callback(ctx, false)
	if err != nil {
		return DownloadSFTP{}, fault("HOST_KEY_UNAVAILABLE", "无法读取主机信任记录")
	}
	var root *string
	for _, protocol := range prepared.Asset.Protocols {
		if protocol.Name == "sftp" {
			root = protocol.Settings.SFTPHome
		}
	}
	client, err := sftpclient.Open(ctx, sftpclient.OpenOptions{Connection: prepared.Connection, HostKeyCallback: callback, Timeout: c.Timeout, RootPath: root, AccountUsername: prepared.Account.Username})
	if err != nil {
		return DownloadSFTP{}, fault("SFTP_UNAVAILABLE", "SFTP 连接不可用")
	}
	return DownloadSFTP{Client: client, Root: root, Account: prepared.Account.Username}, nil
}
