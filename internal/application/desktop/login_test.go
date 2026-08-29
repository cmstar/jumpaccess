package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

type recordingTokens struct {
	profile string
	token   credential.Token
}

func (r *recordingTokens) Save(profile string, token credential.Token) error {
	r.profile = profile
	r.token = token
	return nil
}

func TestLoginCoordinatorCompletesPastedNativeCallback(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/core/auth/oauth2-provider/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(writer).Encode(oauth.Metadata{
				Issuer:                provider.URL,
				ClientID:              "client-id",
				AuthorizationEndpoint: provider.URL + "/authorize",
				TokenEndpoint:         provider.URL + "/token",
			})
		case "/token":
			_ = json.NewEncoder(writer).Encode(oauth.TokenResponse{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer provider.Close()

	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: provider.URL}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	tokens := &recordingTokens{}
	var opened string
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	coordinator := LoginCoordinator{
		Config: store, Tokens: tokens, HTTPClient: provider.Client(), Timeout: time.Minute,
		OpenBrowser: func(rawURL string) error { opened = rawURL; return nil },
		Now:         func() time.Time { return now },
	}

	attempt, err := coordinator.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := url.Parse(opened)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.ID == "" || attempt.Profile != "work" || authorizationURL.Query().Get("state") == "" {
		t.Fatalf("attempt = %#v, opened = %q", attempt, opened)
	}
	callback := oauth.NativeRedirectURI + "?code=authorization-code&state=" + url.QueryEscape(authorizationURL.Query().Get("state"))
	status, err := coordinator.Complete(context.Background(), attempt.ID, callback)
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn || tokens.profile != "work" || tokens.token.AccessToken != "access" || !tokens.token.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("status = %#v, token = %#v", status, tokens.token)
	}
}

func TestLoginCoordinatorKeepsAttemptAfterInvalidCallback(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(oauth.Metadata{
			Issuer: provider.URL, ClientID: "client-id",
			AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token",
		})
	}))
	defer provider.Close()
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: provider.URL}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	coordinator := LoginCoordinator{Config: store, Tokens: &recordingTokens{}, HTTPClient: provider.Client(), OpenBrowser: func(string) error { return nil }}

	attempt, err := coordinator.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Complete(context.Background(), attempt.ID, oauth.NativeRedirectURI+"?code=code&state=wrong"); err == nil {
		t.Fatal("Complete accepted wrong OAuth state")
	}
	if !coordinator.Pending(attempt.ID) {
		t.Fatal("invalid callback removed the pending login attempt")
	}
	if err := coordinator.Cancel(attempt.ID); err != nil {
		t.Fatal(err)
	}
	if coordinator.Pending(attempt.ID) {
		t.Fatal("Cancel left the login attempt pending")
	}
}

func TestLoginCoordinatorClearsExpiredAttempt(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(oauth.Metadata{
			Issuer: provider.URL, ClientID: "client-id",
			AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token",
		})
	}))
	defer provider.Close()
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: provider.URL}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	coordinator := LoginCoordinator{
		Config: store, Tokens: &recordingTokens{}, HTTPClient: provider.Client(),
		OpenBrowser: func(string) error { return nil }, Timeout: 10 * time.Millisecond,
	}
	attempt, err := coordinator.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for coordinator.Pending(attempt.ID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if coordinator.Pending(attempt.ID) {
		t.Fatal("expired login attempt remained pending")
	}
}
