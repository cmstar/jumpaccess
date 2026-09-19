package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

func TestLoginRejectsProfileChangedDuringAuthorization(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://old.example.test"}
	tokens := &memoryTokens{tokens: make(map[string]credential.Token)}
	service := Service{Config: staticConfig{value: configuration}, Tokens: tokens, Manager: Manager{Locker: &mutexLocker{}}}
	service.LoginFlow = func(context.Context, string, LoginOptions) (credential.Token, error) {
		configuration.Profiles["work"] = projectconfig.Profile{URL: "https://new.example.test"}
		return credential.Token{AccessToken: "fixture", Site: "https://old.example.test", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	if _, err := service.Login(context.Background(), "work", LoginOptions{}); err == nil {
		t.Fatal("changed profile accepted a stale authorization")
	}
	if _, err := tokens.Load("work"); !errors.Is(err, credential.ErrNotFound) {
		t.Fatal("stale credential was saved")
	}
}

type deniedLocker struct{ called bool }

func (l *deniedLocker) Lock(context.Context, string) (func() error, error) {
	l.called = true
	return nil, context.Canceled
}

func TestLogoutMustAcquireCredentialLockBeforeRevocation(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	tokens := &memoryTokens{tokens: map[string]credential.Token{"work": {Site: "https://jump.example.test", AccessToken: "fixture"}}}
	locker := &deniedLocker{}
	revoked := false
	service := Service{Config: staticConfig{value: configuration}, Tokens: tokens, Manager: Manager{Locker: locker}, Revoke: func(context.Context, credential.Token) error { revoked = true; return nil }}
	err := service.Logout(context.Background(), "work")
	if !errors.Is(err, context.Canceled) || !locker.called || revoked {
		t.Fatal("logout bypassed the credential lock")
	}
	if _, err := tokens.Load("work"); err != nil {
		t.Fatal("logout removed credentials without acquiring lock")
	}
}

func TestStatusAndRefreshRejectCredentialAfterManualSiteChange(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://new.example.test"}
	tokens := &memoryTokens{tokens: map[string]credential.Token{"work": {Site: "https://old.example.test", AccessToken: "fixture", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}}}
	service := Service{Config: staticConfig{value: configuration}, Tokens: tokens}
	status, err := service.Status("work")
	if err != nil || status.LoggedIn {
		t.Fatal("foreign credential was shown as authenticated")
	}
	manager := Manager{Config: service.Config, Tokens: tokens, Locker: &mutexLocker{}}
	if _, err := manager.EnsureFresh(context.Background(), "work"); !errors.Is(err, ErrLoginRequired) {
		t.Fatal("fresh credential from old site was accepted")
	}
	if _, err := manager.RefreshNow(context.Background(), "work"); !errors.Is(err, ErrLoginRequired) {
		t.Fatal("foreign credential was refreshed")
	}
}
