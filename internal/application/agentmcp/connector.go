package agentmcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/sshhostkey"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"github.com/cmstar/jumpaccess/internal/target"
	"golang.org/x/crypto/ssh"
)

type IdentitySource interface {
	Identity(context.Context, string) (auth.LoginIdentity, error)
	EnsureFresh(context.Context, string) (credential.Token, error)
}

// Connector 只读取明确的目标；不修改当前选择，不发起交互登录或主机信任弹窗。
type Connector struct {
	Config     ConfigReader
	Auth       IdentitySource
	Queries    *Queries
	Connect    connect.Service
	KnownHosts string
	Timeout    time.Duration
}

func (c Connector) Resolve(ctx context.Context, in Target) (ResolvedTarget, error) {
	p, err := c.Queries.profile(in.Profile)
	if err != nil {
		return ResolvedTarget{}, err
	}
	identity, err := c.Auth.Identity(ctx, in.Profile)
	if err != nil {
		return ResolvedTarget{}, queryError(err)
	}
	if in.Alias != "" {
		if in.Organization != "" || in.AssetID != "" || in.AccountID != "" {
			return ResolvedTarget{}, fault("INVALID_ARGUMENT", "Alias 与完整目标不能同时提供")
		}
		a, ok := p.Aliases[in.Alias]
		if !ok {
			return ResolvedTarget{}, fault("TARGET_NOT_FOUND", "Alias 不存在")
		}
		in.Organization = a.Organization
		if in.Organization == "" {
			in.Organization = p.Organization
		}
		if in.Organization == "" || a.Account == "" {
			return ResolvedTarget{}, fault("TARGET_INCOMPLETE", "Alias 必须确定组织和账号")
		}
		asset, err := c.Queries.uniqueAsset(ctx, in.Profile, in.Organization, a.Asset)
		if err != nil {
			return ResolvedTarget{}, fault("TARGET_AMBIGUOUS", "Alias 资产无法唯一确定")
		}
		account, err := connect.ResolvePermittedAccount(asset.Accounts, a.Account)
		if err != nil {
			return ResolvedTarget{}, fault("TARGET_AMBIGUOUS", "Alias 账号无法唯一确定")
		}
		in.AssetID, in.AccountID, in.Alias = asset.ID, account.ID, ""
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
	if err := c.Validate(ctx, r); err != nil {
		return nil, err
	}
	// 固定配置副本中不含 Alias，防止恰好同名的 Alias 覆盖已解析的资产 ID。
	cfg := config.Default()
	cfg.Profiles[r.Profile] = config.Profile{URL: r.Site, Organization: r.Organization}
	service := c.Connect
	service.Config = frozenConfig{cfg}
	service.Tokens = boundTokens{c.Auth, r}
	prepared, err := service.Prepare(ctx, connect.Options{Target: target.Input{Profile: r.Profile, Organization: r.Organization, Target: r.AssetID, Account: r.AccountID}, Protocol: "ssh", NonInteractive: true})
	if err != nil {
		return nil, asFault(err, "CONNECT_FAILED", "无法准备 SSH 连接，请检查目标权限和登录")
	}
	if prepared.Asset.ID != r.AssetID || prepared.Account.ID != r.AccountID {
		return nil, fault("IDENTITY_CHANGED", "连接准备返回了不同目标或账号")
	}
	if err := c.Validate(ctx, r); err != nil {
		return nil, err
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
