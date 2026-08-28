package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

type staticConfig struct{ value projectconfig.Config }

func (s staticConfig) Load() (projectconfig.Config, error) { return s.value, nil }

func TestServiceLoginDefaultsToManualFlowAndStoresNativeCredential(t *testing.T) {
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	store := &memoryTokens{tokens: make(map[string]credential.Token)}
	service := Service{
		Config: staticConfig{value: value}, Tokens: store,
		ManualLoginFlow: func(_ context.Context, site string) (credential.Token, error) {
			if site != "https://jump.example.test" {
				t.Fatalf("site = %q", site)
			}
			return credential.Token{AccessToken: "secret", Site: site}, nil
		},
	}

	status, err := service.Login(context.Background(), "", LoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Profile != "work" || !status.LoggedIn {
		t.Fatalf("status = %#v", status)
	}
	stored, err := store.Load("work")
	if err != nil || stored.AccessToken != "secret" {
		t.Fatalf("stored token = %#v, %v", stored, err)
	}
}

func TestServiceStatusReportsMissingCredentialWithoutExposingAnError(t *testing.T) {
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	service := Service{Config: staticConfig{value: value}, Tokens: &memoryTokens{tokens: make(map[string]credential.Token)}}

	status, err := service.Status("")
	if err != nil {
		t.Fatal(err)
	}
	if status.LoggedIn || status.Profile != "work" {
		t.Fatalf("status = %#v", status)
	}
}

func TestServiceLogoutRevokesBeforeDeletingCredential(t *testing.T) {
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	store := &memoryTokens{tokens: map[string]credential.Token{"work": {AccessToken: "access", RefreshToken: "refresh"}}}
	var revoked bool
	service := Service{
		Config: staticConfig{value: value}, Tokens: store,
		Revoke: func(_ context.Context, token credential.Token) error {
			revoked = token.RefreshToken == "refresh"
			return nil
		},
	}

	if err := service.Logout(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("credential was not revoked")
	}
	if _, err := store.Load("work"); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("credential still exists: %v", err)
	}
}

func TestServiceStatusMarksExpiredToken(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	service := Service{
		Config: staticConfig{value: value},
		Tokens: &memoryTokens{tokens: map[string]credential.Token{"work": {AccessToken: "access", ExpiresAt: now.Add(-time.Second)}}},
		Now:    func() time.Time { return now },
	}

	status, err := service.Status("")
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn || !status.Expired {
		t.Fatalf("status = %#v", status)
	}
}

func TestServiceLoginAppliesOAuthTimeout(t *testing.T) {
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	service := Service{
		Config: staticConfig{value: value}, Tokens: &memoryTokens{tokens: make(map[string]credential.Token)}, Timeout: time.Minute,
		LoginFlow: func(ctx context.Context, _ string) (credential.Token, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("login context has no deadline")
			}
			return credential.Token{AccessToken: "secret"}, nil
		},
	}
	if _, err := service.Login(context.Background(), "", LoginOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceLoginManualOptionSelectsPermanentFallbackFlow(t *testing.T) {
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	var selected string
	service := Service{
		Config: staticConfig{value: value}, Tokens: &memoryTokens{tokens: make(map[string]credential.Token)},
		LoginFlow: func(context.Context, string) (credential.Token, error) {
			selected = "native"
			return credential.Token{AccessToken: "native"}, nil
		},
		ManualLoginFlow: func(context.Context, string) (credential.Token, error) {
			selected = "manual"
			return credential.Token{AccessToken: "manual"}, nil
		},
	}

	if _, err := service.Login(context.Background(), "", LoginOptions{Manual: true}); err != nil {
		t.Fatal(err)
	}
	if selected != "manual" {
		t.Fatalf("selected flow = %q", selected)
	}
}
