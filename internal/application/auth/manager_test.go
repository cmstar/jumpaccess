package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

type memoryTokens struct {
	mu     sync.Mutex
	tokens map[string]credential.Token
}

func (m *memoryTokens) Load(profile string) (credential.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	token, ok := m.tokens[profile]
	if !ok {
		return credential.Token{}, credential.ErrNotFound
	}
	return token, nil
}

func (m *memoryTokens) Save(profile string, token credential.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[profile] = token
	return nil
}

func (m *memoryTokens) Delete(profile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, profile)
	return nil
}

type mutexLocker struct{ mu sync.Mutex }

func (l *mutexLocker) Lock(context.Context, string) (func() error, error) {
	l.mu.Lock()
	return func() error { l.mu.Unlock(); return nil }, nil
}

func TestEnsureFreshReturnsUnexpiredTokenWithoutRefresh(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := &memoryTokens{tokens: map[string]credential.Token{
		"work": {AccessToken: "still-valid", ExpiresAt: now.Add(10 * time.Minute)},
	}}
	manager := Manager{
		Tokens:        store,
		Now:           func() time.Time { return now },
		RefreshBefore: time.Minute,
		Refresh: func(context.Context, credential.Token) (oauth.TokenResponse, error) {
			t.Fatal("Refresh was called for a fresh token")
			return oauth.TokenResponse{}, nil
		},
	}

	got, err := manager.EnsureFresh(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "still-valid" {
		t.Fatalf("token = %#v", got)
	}
}

func TestEnsureFreshRotatesExpiringTokenAndPreservesOmittedRefreshToken(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := &memoryTokens{tokens: map[string]credential.Token{
		"work": {
			AccessToken: "old-access", RefreshToken: "old-refresh", TokenType: "Bearer",
			ClientID: "client-id", Site: "https://jump.example.test", ExpiresAt: now.Add(30 * time.Second),
		},
	}}
	manager := Manager{
		Tokens:        store,
		Locker:        &mutexLocker{},
		Now:           func() time.Time { return now },
		RefreshBefore: time.Minute,
		Refresh: func(_ context.Context, old credential.Token) (oauth.TokenResponse, error) {
			if old.RefreshToken != "old-refresh" {
				t.Fatalf("old token = %#v", old)
			}
			return oauth.TokenResponse{AccessToken: "new-access", ExpiresIn: 3600}, nil
		},
	}

	got, err := manager.EnsureFresh(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "old-refresh" || got.TokenType != "Bearer" {
		t.Fatalf("refreshed token = %#v", got)
	}
	saved, _ := store.Load("work")
	if saved != got {
		t.Fatalf("saved token = %#v, returned %#v", saved, got)
	}
}

func TestEnsureFreshSerializesConcurrentRefreshAndReloadsAfterLock(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := &memoryTokens{tokens: map[string]credential.Token{
		"work": {AccessToken: "old", RefreshToken: "refresh", ClientID: "cid", Site: "https://jump.example.test", ExpiresAt: now},
	}}
	var refreshCalls atomic.Int32
	manager := Manager{
		Tokens: store, Locker: &mutexLocker{}, Now: func() time.Time { return now }, RefreshBefore: time.Minute,
		Refresh: func(context.Context, credential.Token) (oauth.TokenResponse, error) {
			refreshCalls.Add(1)
			time.Sleep(20 * time.Millisecond)
			return oauth.TokenResponse{AccessToken: "new", RefreshToken: "rotated", ExpiresIn: 3600}, nil
		},
	}

	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := manager.EnsureFresh(context.Background(), "work")
			errorsCh <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestEnsureFreshTurnsMissingOrUnrefreshableCredentialIntoLoginRequired(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	for name, tokens := range map[string]map[string]credential.Token{
		"missing": {},
		"no refresh token": {
			"work": {AccessToken: "expired", ExpiresAt: now.Add(-time.Minute)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			manager := Manager{Tokens: &memoryTokens{tokens: tokens}, Now: func() time.Time { return now }, RefreshBefore: time.Minute}
			_, err := manager.EnsureFresh(context.Background(), "work")
			if !errors.Is(err, ErrLoginRequired) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRefreshNowRefreshesEvenWhenAccessTokenIsStillValid(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := &memoryTokens{tokens: map[string]credential.Token{
		"work": {AccessToken: "old", RefreshToken: "refresh", ClientID: "cid", Site: "https://jump.example.test", ExpiresAt: now.Add(time.Hour)},
	}}
	var called bool
	manager := Manager{
		Tokens: store, Locker: &mutexLocker{}, Now: func() time.Time { return now }, RefreshBefore: time.Minute,
		Refresh: func(context.Context, credential.Token) (oauth.TokenResponse, error) {
			called = true
			return oauth.TokenResponse{AccessToken: "new", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
		},
	}

	got, err := manager.RefreshNow(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if !called || got.AccessToken != "new" {
		t.Fatalf("called = %v, token = %#v", called, got)
	}
}

func TestSuperviseReportsRefreshFailuresWithoutOwningSessionCancellation(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := &memoryTokens{tokens: map[string]credential.Token{
		"work": {AccessToken: "old", RefreshToken: "refresh", ClientID: "cid", Site: "https://jump.example.test", ExpiresAt: now},
	}}
	manager := Manager{
		Tokens: store, Locker: &mutexLocker{}, Now: func() time.Time { return now }, RefreshBefore: time.Minute,
		Refresh: func(context.Context, credential.Token) (oauth.TokenResponse, error) {
			return oauth.TokenResponse{}, errors.New("provider unavailable")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var warnings atomic.Int32
	go func() {
		manager.Supervise(ctx, "work", time.Millisecond, func(error) {
			if warnings.Add(1) == 2 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Supervise did not stop with its own context")
	}
	if warnings.Load() < 2 {
		t.Fatalf("warnings = %d, want at least 2", warnings.Load())
	}
}

func TestEnsureFreshMapsInvalidGrantToLoginRequired(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store := &memoryTokens{tokens: map[string]credential.Token{
		"work": {AccessToken: "old", RefreshToken: "refresh", ExpiresAt: now},
	}}
	manager := Manager{
		Tokens: store, Locker: &mutexLocker{}, Now: func() time.Time { return now }, RefreshBefore: time.Minute,
		Refresh: func(context.Context, credential.Token) (oauth.TokenResponse, error) {
			return oauth.TokenResponse{}, &oauth.TokenEndpointError{Code: "invalid_grant", StatusCode: 400}
		},
	}
	_, err := manager.EnsureFresh(context.Background(), "work")
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("error = %v", err)
	}
}
