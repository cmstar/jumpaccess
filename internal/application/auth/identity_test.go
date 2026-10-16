package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

func TestLoginIdentityChangesOnlyOnNewLogin(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Profiles["work"] = config.Profile{URL: "https://jump.example.test"}
	store := &memoryTokens{tokens: map[string]credential.Token{}}
	locker := &mutexLocker{}
	manager := Manager{Config: staticConfig{value: cfg}, Tokens: store, Locker: locker, Refresh: func(context.Context, credential.Token) (oauth.TokenResponse, error) {
		return oauth.TokenResponse{AccessToken: "refreshed", ExpiresIn: 3600}, nil
	}}
	token := credential.Token{Site: cfg.Profiles["work"].URL, AccessToken: "fixture", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
	if err := CommitLogin(ctx, manager.Config, store, locker, "work", token); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Identity(ctx, "work")
	if err != nil || first.LoginID == "" {
		t.Fatal("登录缺少代际", err)
	}
	if _, err := manager.RefreshNow(ctx, "work"); err != nil {
		t.Fatal(err)
	}
	afterRefresh, err := manager.Identity(ctx, "work")
	if err != nil || afterRefresh != first {
		t.Fatal("普通刷新改变了登录身份", err)
	}
	if err := CommitLogin(ctx, manager.Config, store, locker, "work", token); err != nil {
		t.Fatal(err)
	}
	afterLogin, err := manager.Identity(ctx, "work")
	if err != nil || afterLogin.LoginID == first.LoginID {
		t.Fatal("重新登录未改变代际", err)
	}
	if err := store.Delete("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Identity(ctx, "work"); err == nil {
		t.Fatal("退出后仍有身份")
	}
}

func TestLegacyIdentityMigrationIsSharedAndDoesNotUseAccessToken(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Profiles["work"] = config.Profile{URL: "https://jump.example.test"}
	store := &memoryTokens{tokens: map[string]credential.Token{"work": {Site: cfg.Profiles["work"].URL, AccessToken: "fixture", ExpiresAt: time.Now().Add(time.Hour)}}}
	manager := Manager{Config: staticConfig{value: cfg}, Tokens: store, Locker: &mutexLocker{}}
	results := make(chan LoginIdentity, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			identity, err := manager.Identity(ctx, "work")
			if err != nil {
				t.Error(err)
				return
			}
			results <- identity
		}()
	}
	wg.Wait()
	close(results)
	var first LoginIdentity
	for identity := range results {
		if first.LoginID == "" {
			first = identity
		}
		if identity != first {
			t.Fatal("并发迁移创建了不同身份")
		}
	}
	if first.LoginID == "" || first.LoginID == "fixture" {
		t.Fatal("代际必须是独立的非秘密标识")
	}
	saved, err := store.Load("work")
	if err != nil || saved.LoginID != first.LoginID || saved.AccessToken != "fixture" {
		t.Fatal("旧凭据迁移不正确", err)
	}
}
