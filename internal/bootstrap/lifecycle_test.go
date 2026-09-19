package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

type observedLocker struct {
	authapp.Locker
	attempts chan string
}

func (l observedLocker) Lock(ctx context.Context, key string) (func() error, error) {
	l.attempts <- key
	return l.Locker.Lock(ctx, key)
}

func TestProfileMutationSerializesWithRefreshAcrossRuntimeInstances(t *testing.T) {
	for _, mutation := range []string{"url", "delete", "logout"} {
		t.Run(mutation, func(t *testing.T) {
			root := t.TempDir()
			first, err := New(Options{RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			if err := first.Settings.AddProfile("work", "https://old.example.test"); err != nil {
				t.Fatal(err)
			}
			if err := first.Tokens.Save("work", credential.Token{Site: "https://old.example.test", ClientID: "fixture", AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}
			second, err := New(Options{RootDir: root})
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			attempts := make(chan string, 4)
			first.AuthManager.Locker = observedLocker{Locker: first.AuthManager.Locker, attempts: attempts}
			second.Settings.Locker = observedLocker{Locker: second.Settings.Locker, attempts: attempts}
			second.Auth.Manager.Locker = observedLocker{Locker: second.Auth.Manager.Locker, attempts: attempts}
			first.AuthManager.Refresh = func(context.Context, credential.Token) (oauth.TokenResponse, error) {
				close(entered)
				<-release
				return oauth.TokenResponse{AccessToken: "new", RefreshToken: "rotated", ExpiresIn: 3600}, nil
			}
			refreshed := make(chan error, 1)
			go func() { _, err := first.AuthManager.EnsureFresh(context.Background(), "work"); refreshed <- err }()
			<-entered
			refreshKey := <-attempts
			revoked := ""
			second.Auth.Revoke = func(_ context.Context, token credential.Token) error { revoked = token.AccessToken; return nil }
			mutated := make(chan error, 1)
			go func() {
				switch mutation {
				case "url":
					mutated <- second.Settings.UpdateProfileURL("work", "https://new.example.test")
				case "delete":
					mutated <- second.Settings.DeleteProfile("work")
				case "logout":
					mutated <- second.Auth.Logout(context.Background(), "work")
				}
			}()
			select {
			case key := <-attempts:
				if key != refreshKey {
					t.Fatal("mutation uses a different credential lock")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("mutation did not acquire credential lock")
			}
			// 等待凭据锁时不得持配置锁，否则与刷新读取配置形成锁反序。
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := first.Store.Update(ctx, func(*projectconfig.Config) error { return nil }); err != nil {
				t.Fatal("mutation held configuration lock while waiting for credential lock")
			}
			close(release)
			if err := <-refreshed; err != nil {
				t.Fatal(err)
			}
			if err := <-mutated; err != nil {
				t.Fatal(err)
			}
			if _, err := first.Tokens.Load("work"); !errors.Is(err, credential.ErrNotFound) {
				t.Fatal("refresh resurrected removed credentials")
			}
			if mutation == "logout" && revoked != "new" {
				t.Fatal("logout revoked an outdated credential")
			}
		})
	}
}
