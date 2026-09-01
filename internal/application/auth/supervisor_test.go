package auth

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

type mutableConfigLoader struct {
	mu    sync.Mutex
	value projectconfig.Config
}

func (l *mutableConfigLoader) Load() (projectconfig.Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.value, nil
}

type recordingFreshener struct {
	calls chan string
}

type freshenerFunc func(context.Context, string) (credential.Token, error)

func (f freshenerFunc) EnsureFresh(ctx context.Context, profile string) (credential.Token, error) {
	return f(ctx, profile)
}

func (f recordingFreshener) EnsureFresh(_ context.Context, profile string) (credential.Token, error) {
	f.calls <- profile
	return credential.Token{}, nil
}

func TestProfileSupervisorKeepsEveryRefreshCredentialFreshWhileRunning(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://work.example.test"}
	configuration.Profiles["staging"] = projectconfig.Profile{URL: "https://staging.example.test"}
	configuration.Profiles["access-only"] = projectconfig.Profile{URL: "https://legacy.example.test"}
	config := &mutableConfigLoader{value: configuration}
	credentials := &memoryTokens{tokens: map[string]credential.Token{
		"work":        {RefreshToken: "work-refresh", RefreshedAt: time.Now()},
		"access-only": {AccessToken: "access-only"},
	}}
	calls := make(chan string, 16)
	supervisor := ProfileSupervisor{
		Config:      config,
		Credentials: credentials,
		Freshener:   recordingFreshener{calls: calls},
		Interval:    5 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		supervisor.Run(ctx)
		close(done)
	}()

	if got := receiveProfile(t, calls); got != "work" {
		t.Fatalf("first refreshed profile = %q, want work", got)
	}
	credentials.mu.Lock()
	credentials.tokens["staging"] = credential.Token{RefreshToken: "staging-refresh", RefreshedAt: time.Now()}
	credentials.mu.Unlock()

	deadline := time.After(time.Second)
	foundStaging := false
	for !foundStaging {
		select {
		case profile := <-calls:
			foundStaging = profile == "staging"
		case <-deadline:
			t.Fatal("supervisor did not discover the newly authenticated staging profile")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProfileSupervisor did not stop after cancellation")
	}
}

func TestProfileSupervisorWaitsForNewCredentialsAfterLoginIsRequired(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://work.example.test"}
	config := &mutableConfigLoader{value: configuration}
	refreshedAt := time.Now()
	credentials := &memoryTokens{tokens: map[string]credential.Token{
		"work": {RefreshToken: "expired-refresh", RefreshedAt: refreshedAt},
	}}
	calls := make(chan string, 8)
	supervisor := ProfileSupervisor{
		Config:      config,
		Credentials: credentials,
		Freshener: freshenerFunc(func(_ context.Context, profile string) (credential.Token, error) {
			calls <- profile
			return credential.Token{}, fmt.Errorf("%w for profile %q", ErrLoginRequired, profile)
		}),
		Interval: 5 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		supervisor.Run(ctx)
		close(done)
	}()

	if got := receiveProfile(t, calls); got != "work" {
		t.Fatalf("first refreshed profile = %q, want work", got)
	}
	select {
	case profile := <-calls:
		t.Fatalf("supervisor retried unchanged invalid credentials for %q", profile)
	case <-time.After(30 * time.Millisecond):
	}

	credentials.mu.Lock()
	credentials.tokens["work"] = credential.Token{RefreshToken: "new-refresh", RefreshedAt: refreshedAt.Add(time.Minute)}
	credentials.mu.Unlock()
	if got := receiveProfile(t, calls); got != "work" {
		t.Fatalf("refreshed profile after login = %q, want work", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProfileSupervisor did not stop after cancellation")
	}
}

func receiveProfile(t *testing.T, calls <-chan string) string {
	t.Helper()
	select {
	case profile := <-calls:
		return profile
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an automatic refresh check")
		return ""
	}
}
