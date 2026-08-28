package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestManualFlowCompletesAuthorizationFromPastedCallback(t *testing.T) {
	var tokenForm url.Values
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
			tokenForm = r.Form
			_ = json.NewEncoder(w).Encode(TokenResponse{
				AccessToken: "access", RefreshToken: "refresh", TokenType: "Bearer", ExpiresIn: 3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	callbackReader, callbackWriter := io.Pipe()
	defer callbackReader.Close()
	var instructions bytes.Buffer
	var openedURL string
	flow := ManualFlow{
		HTTPClient:  provider.Client(),
		RedirectURI: NativeRedirectURI,
		Input:       callbackReader,
		Output:      &instructions,
		OpenBrowser: func(rawURL string) error {
			openedURL = rawURL
			authorizationURL, _ := url.Parse(rawURL)
			state := authorizationURL.Query().Get("state")
			callback := NativeRedirectURI + "?code=authorization-code&state=" + url.QueryEscape(state)
			go func() {
				_, _ = io.WriteString(callbackWriter, callback+"\n")
				_ = callbackWriter.Close()
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
	authorizationURL, err := url.Parse(openedURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorizationURL.Query().Get("redirect_uri"); got != NativeRedirectURI {
		t.Fatalf("authorization redirect_uri = %q", got)
	}
	if tokenForm.Get("redirect_uri") != NativeRedirectURI || tokenForm.Get("code_verifier") == "" {
		t.Fatalf("token form = %#v", tokenForm)
	}
	if !strings.Contains(instructions.String(), "do not select Confirm") || strings.Contains(instructions.String(), "access") {
		t.Fatalf("instructions = %q", instructions.String())
	}
}

func TestManualFlowRejectsCallbackWithWrongState(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Metadata{
			Issuer: provider.URL, ClientID: "client-id",
			AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token",
		})
	}))
	defer provider.Close()

	flow := ManualFlow{
		HTTPClient:  provider.Client(),
		RedirectURI: NativeRedirectURI,
		Input:       strings.NewReader(NativeRedirectURI + "?code=authorization-code&state=wrong\n"),
		Output:      io.Discard,
		OpenBrowser: func(string) error { return nil },
	}

	if _, err := flow.Login(context.Background(), provider.URL); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("Login error = %v", err)
	}
}
