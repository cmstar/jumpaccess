package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

type TokenSaver interface {
	Save(string, credential.Token) error
}

type LoginAttempt struct {
	ID        string `json:"id"`
	Profile   string `json:"profile"`
	ExpiresAt string `json:"expiresAt"`
}

type pendingLogin struct {
	profile       string
	authorization oauth.ManualAuthorization
	expiresAt     time.Time
	timer         *time.Timer
	completing    bool
}

type LoginCoordinator struct {
	Config      ConfigLoader
	Tokens      TokenSaver
	Locker      authapp.Locker
	HTTPClient  *http.Client
	OpenBrowser func(string) error
	Timeout     time.Duration
	Now         func() time.Time

	mu      sync.Mutex
	pending map[string]pendingLogin
}

func (c *LoginCoordinator) Start(ctx context.Context, requestedProfile string) (LoginAttempt, error) {
	if c.OpenBrowser == nil {
		return LoginAttempt{}, fmt.Errorf("browser opener is unavailable")
	}
	configuration, err := c.Config.Load()
	if err != nil {
		return LoginAttempt{}, err
	}
	profileName := requestedProfile
	if profileName == "" {
		profileName = configuration.CurrentProfile
	}
	profile, exists := configuration.Profiles[profileName]
	if !exists {
		return LoginAttempt{}, fmt.Errorf("profile %q does not exist", profileName)
	}
	authorization, err := oauth.BeginManualAuthorization(ctx, c.HTTPClient, profile.URL, oauth.NativeRedirectURI)
	if err != nil {
		return LoginAttempt{}, err
	}
	if err := c.OpenBrowser(authorization.URL); err != nil {
		return LoginAttempt{}, fmt.Errorf("open authorization URL: %w", err)
	}
	id, err := loginAttemptID()
	if err != nil {
		return LoginAttempt{}, err
	}
	now := c.now()
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	expiresAt := now.Add(timeout)
	c.mu.Lock()
	if c.pending == nil {
		c.pending = make(map[string]pendingLogin)
	}
	for existingID, attempt := range c.pending {
		if attempt.profile == profileName {
			if attempt.timer != nil {
				attempt.timer.Stop()
			}
			delete(c.pending, existingID)
		}
	}
	timer := time.AfterFunc(timeout, func() { c.expire(id) })
	c.pending[id] = pendingLogin{profile: profileName, authorization: authorization, expiresAt: expiresAt, timer: timer}
	c.mu.Unlock()
	return LoginAttempt{ID: id, Profile: profileName, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)}, nil
}

func (c *LoginCoordinator) Complete(ctx context.Context, id, rawCallback string) (AuthStatus, error) {
	c.mu.Lock()
	attempt, exists := c.pending[id]
	if exists && attempt.completing {
		c.mu.Unlock()
		return AuthStatus{}, fmt.Errorf("OAuth login attempt is already completing")
	}
	if exists {
		attempt.completing = true
		c.pending[id] = attempt
	}
	c.mu.Unlock()
	if !exists {
		return AuthStatus{}, fmt.Errorf("OAuth login attempt is not pending")
	}
	if !attempt.expiresAt.After(c.now()) {
		_ = c.Cancel(id)
		return AuthStatus{}, fmt.Errorf("OAuth login attempt expired")
	}
	token, err := attempt.authorization.Complete(ctx, rawCallback, c.now())
	if err != nil {
		c.mu.Lock()
		if current, pending := c.pending[id]; pending {
			current.completing = false
			c.pending[id] = current
		}
		c.mu.Unlock()
		return AuthStatus{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, pending := c.pending[id]; !pending || !attempt.expiresAt.After(c.now()) {
		return AuthStatus{}, fmt.Errorf("OAuth login attempt is no longer pending")
	}
	commitContext, cancel := context.WithTimeout(ctx, attempt.expiresAt.Sub(c.now()))
	defer cancel()
	if err := authapp.CommitLogin(commitContext, c.Config, c.Tokens, c.Locker, attempt.profile, token); err != nil {
		attempt.completing = false
		c.pending[id] = attempt
		return AuthStatus{}, err
	}
	if attempt.timer != nil {
		attempt.timer.Stop()
	}
	delete(c.pending, id)
	return AuthStatus{
		LoggedIn:         true,
		RefreshAvailable: token.RefreshToken != "",
		ExpiresAt:        token.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

func (c *LoginCoordinator) Cancel(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	attempt, exists := c.pending[id]
	if !exists {
		return fmt.Errorf("OAuth login attempt is not pending")
	}
	if attempt.timer != nil {
		attempt.timer.Stop()
	}
	delete(c.pending, id)
	return nil
}

func (c *LoginCoordinator) expire(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *LoginCoordinator) Pending(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.pending[id]
	return exists
}

func (c *LoginCoordinator) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func loginAttemptID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate OAuth login attempt ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}
