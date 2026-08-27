package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBrowserFlowCompletesLoopbackAuthorization(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case discoveryPath:
			_ = json.NewEncoder(w).Encode(Metadata{
				Issuer:                provider.URL,
				ClientID:              "client-id",
				AuthorizationEndpoint: provider.URL + "/authorize",
				TokenEndpoint:         provider.URL + "/token",
			})
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "authorization-code" || r.Form.Get("code_verifier") == "" {
				t.Fatalf("token form = %#v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	opened := make(chan string, 1)
	flow := BrowserFlow{
		HTTPClient:  provider.Client(),
		CallbackURL: "http://127.0.0.1:0/auth/callback",
		OpenBrowser: func(rawURL string) error {
			opened <- rawURL
			authorizationURL, _ := url.Parse(rawURL)
			callback := authorizationURL.Query().Get("redirect_uri")
			state := authorizationURL.Query().Get("state")
			go func() {
				_, _ = provider.Client().Get(callback + "?code=authorization-code&state=" + url.QueryEscape(state))
			}()
			return nil
		},
		Now: func() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) },
	}

	token, err := flow.Login(context.Background(), provider.URL)
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if token.AccessToken != "access" || token.ClientID != "client-id" || token.Site != provider.URL {
		t.Fatalf("token = %#v", token)
	}
	authorizationURL := <-opened
	if !strings.HasPrefix(authorizationURL, provider.URL+"/authorize?") {
		t.Fatalf("opened URL = %q", authorizationURL)
	}
}

func TestBrowserFlowRejectsCallbackWithMissingState(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Metadata{
			Issuer: provider.URL, ClientID: "client-id",
			AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token",
		})
	}))
	defer provider.Close()

	flow := BrowserFlow{
		HTTPClient:  provider.Client(),
		CallbackURL: "http://127.0.0.1:0/auth/callback",
		OpenBrowser: func(rawURL string) error {
			authorizationURL, _ := url.Parse(rawURL)
			callback := authorizationURL.Query().Get("redirect_uri")
			go func() { _, _ = provider.Client().Get(callback + "?code=authorization-code") }()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := flow.Login(ctx, provider.URL); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("Login error = %v", err)
	}
}
