package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

// LockProfile 统一所有凭据写操作的跨进程互斥；同时需要配置锁时先取得此锁。
func LockProfile(ctx context.Context, locker Locker, profile string) (func() error, error) {
	if locker == nil {
		return nil, fmt.Errorf("OAuth credential lock is unavailable")
	}
	digest := sha256.Sum256([]byte(profile))
	return locker.Lock(ctx, fmt.Sprintf("oauth-%x", digest))
}

// ValidateTokenSite 在构造 API 请求前绑定凭据和站点，防止配置变更后发送旧站点的 Token。
func ValidateTokenSite(token credential.Token, site string) error {
	actual, err := projectconfig.NormalizeProfileURL(token.Site)
	if err != nil {
		return fmt.Errorf("%w: OAuth credential site is invalid", ErrLoginRequired)
	}
	expected, err := projectconfig.NormalizeProfileURL(site)
	if err != nil || actual != expected {
		return fmt.Errorf("%w: OAuth credential belongs to another site", ErrLoginRequired)
	}
	return nil
}

type TokenSaver interface {
	Save(string, credential.Token) error
}

// CommitLogin 供 CLI 与桌面登录共用。浏览器交互不占锁，提交时重新核对 Profile。
func CommitLogin(ctx context.Context, config ConfigLoader, tokens TokenSaver, locker Locker, profile string, token credential.Token) error {
	unlock, err := LockProfile(ctx, locker, profile)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	return commitLoginLocked(ctx, config, tokens, profile, token)
}

func commitLoginLocked(ctx context.Context, config ConfigLoader, tokens TokenSaver, profile string, token credential.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := config.Load()
	if err != nil {
		return err
	}
	configured, exists := value.Profiles[profile]
	if !exists {
		return fmt.Errorf("profile %q no longer exists", profile)
	}
	if err := ValidateTokenSite(token, configured.URL); err != nil {
		return err
	}
	token.LoginID, err = newLoginID()
	if err != nil {
		return err
	}
	if err := tokens.Save(profile, token); err != nil {
		return fmt.Errorf("save OAuth credential: %w", err)
	}
	return nil
}

// LoginIdentity 代表一次本地登录，不推断未验证的远端用户名或 JWT 声明。
type LoginIdentity struct {
	Profile string
	Site    string
	LoginID string
}

// Identity 供需要固定登录上下文的业务使用；旧凭据在 Profile 锁内只补齐一次代际。
func (m Manager) Identity(ctx context.Context, profile string) (LoginIdentity, error) {
	if _, err := m.EnsureFresh(ctx, profile); err != nil {
		return LoginIdentity{}, err
	}
	unlock, err := LockProfile(ctx, m.Locker, profile)
	if err != nil {
		return LoginIdentity{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return LoginIdentity{}, err
	}
	token, err := m.Tokens.Load(profile)
	if err != nil {
		if errors.Is(err, credential.ErrNotFound) {
			return LoginIdentity{}, fmt.Errorf("%w: 登录凭据已失效", ErrLoginRequired)
		}
		return LoginIdentity{}, fmt.Errorf("读取登录凭据失败: %w", err)
	}
	if err := m.validateSite(profile, token); err != nil {
		return LoginIdentity{}, err
	}
	if token.LoginID == "" {
		token.LoginID, err = newLoginID()
		if err != nil {
			return LoginIdentity{}, err
		}
		if err := m.Tokens.Save(profile, token); err != nil {
			return LoginIdentity{}, fmt.Errorf("保存登录代际失败: %w", err)
		}
	}
	return LoginIdentity{Profile: profile, Site: token.Site, LoginID: token.LoginID}, nil
}

func newLoginID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("生成登录代际失败: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
