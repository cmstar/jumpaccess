package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

var ErrLoginRequired = errors.New("login required")

type TokenRepository interface {
	Load(profile string) (credential.Token, error)
	Save(profile string, token credential.Token) error
	Delete(profile string) error
}

type Locker interface {
	Lock(ctx context.Context, key string) (unlock func() error, err error)
}

type Manager struct {
	Tokens        TokenRepository
	Locker        Locker
	Refresh       func(context.Context, credential.Token) (oauth.TokenResponse, error)
	Now           func() time.Time
	RefreshBefore time.Duration
}

func (m Manager) EnsureFresh(ctx context.Context, profile string) (credential.Token, error) {
	return m.ensure(ctx, profile, false)
}

func (m Manager) RefreshNow(ctx context.Context, profile string) (credential.Token, error) {
	return m.ensure(ctx, profile, true)
}

// Supervise refreshes credentials for future API requests. Its context belongs
// to the supervisor, not to an established SSH session, so a refresh failure is
// reported but never closes an active connection.
func (m Manager) Supervise(ctx context.Context, profile string, interval time.Duration, onError func(error)) {
	if interval <= 0 {
		return
	}
	check := func() {
		if _, err := m.EnsureFresh(ctx, profile); err != nil && onError != nil && !errors.Is(err, context.Canceled) {
			onError(err)
		}
	}
	check()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func (m Manager) ensure(ctx context.Context, profile string, force bool) (credential.Token, error) {
	token, err := m.Tokens.Load(profile)
	if err != nil {
		if errors.Is(err, credential.ErrNotFound) {
			return credential.Token{}, fmt.Errorf("%w for profile %q; run jumpctl auth login", ErrLoginRequired, profile)
		}
		return credential.Token{}, fmt.Errorf("load OAuth credential: %w", err)
	}
	if !force && m.isFresh(token) {
		return token, nil
	}
	if token.RefreshToken == "" {
		return credential.Token{}, fmt.Errorf("%w for profile %q; run jumpctl auth login", ErrLoginRequired, profile)
	}
	if m.Locker == nil {
		return credential.Token{}, fmt.Errorf("OAuth refresh lock is unavailable")
	}

	unlock, err := m.Locker.Lock(ctx, "oauth-"+profile)
	if err != nil {
		return credential.Token{}, fmt.Errorf("acquire OAuth refresh lock: %w", err)
	}
	defer func() { _ = unlock() }()

	// Another process may have refreshed this profile while this process waited.
	token, err = m.Tokens.Load(profile)
	if err != nil {
		return credential.Token{}, fmt.Errorf("reload OAuth credential: %w", err)
	}
	if !force && m.isFresh(token) {
		return token, nil
	}
	if token.RefreshToken == "" || m.Refresh == nil {
		return credential.Token{}, fmt.Errorf("%w for profile %q; run jumpctl auth login", ErrLoginRequired, profile)
	}

	response, err := m.Refresh(ctx, token)
	if err != nil {
		var endpointError *oauth.TokenEndpointError
		if errors.As(err, &endpointError) && endpointError.Code == "invalid_grant" {
			return credential.Token{}, fmt.Errorf("%w for profile %q; refresh credential expired or was revoked", ErrLoginRequired, profile)
		}
		return credential.Token{}, fmt.Errorf("refresh OAuth credential: %w", err)
	}
	refreshed, err := response.Record(token.ClientID, token.Site, m.now())
	if err != nil {
		return credential.Token{}, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = token.RefreshToken
	}
	if refreshed.TokenType == "" {
		refreshed.TokenType = token.TokenType
	}
	if err := m.Tokens.Save(profile, refreshed); err != nil {
		return credential.Token{}, fmt.Errorf("save refreshed OAuth credential: %w", err)
	}
	return refreshed, nil
}

func (m Manager) isFresh(token credential.Token) bool {
	return token.ExpiresAt.After(m.now().Add(m.RefreshBefore))
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
