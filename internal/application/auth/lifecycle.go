package auth

import (
	"context"
	"crypto/sha256"
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
	if err := tokens.Save(profile, token); err != nil {
		return fmt.Errorf("save OAuth credential: %w", err)
	}
	return nil
}
