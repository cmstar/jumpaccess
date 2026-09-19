package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type instructionWriter func([]byte) (int, error)

func (write instructionWriter) Write(p []byte) (int, error) { return write(p) }

func TestManualFlowNoBrowserCompletesFromPrintedURL(t *testing.T) {
	for _, mode := range []string{"opener present", "opener absent", "wrong state"} {
		t.Run(mode, func(t *testing.T) {
			var instructions, input bytes.Buffer
			var authorizationURL *url.URL
			exchanges := 0
			var provider *httptest.Server
			provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case discoveryPath:
					_ = json.NewEncoder(w).Encode(Metadata{
						ClientID: "client-id", AuthorizationEndpoint: provider.URL + "/authorize", TokenEndpoint: provider.URL + "/token",
					})
				case "/token":
					exchanges++
					_ = r.ParseForm()
					digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					if r.Form.Get("code") != "test-code" || r.Form.Get("redirect_uri") != NativeRedirectURI ||
						base64.RawURLEncoding.EncodeToString(digest[:]) != authorizationURL.Query().Get("code_challenge") {
						t.Error("token exchange did not preserve the authorization code, redirect URI and PKCE verifier")
					}
					_ = json.NewEncoder(w).Encode(TokenResponse{AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresIn: 3600})
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()
			flow := ManualFlow{
				HTTPClient: provider.Client(), NoBrowser: true, Input: &input,
				OpenBrowser: func(string) error { t.Fatal("no-browser flow attempted to open a browser"); return nil },
				Output: instructionWriter(func(p []byte) (int, error) {
					// 模拟用户把终端打印的地址拿到外部浏览器授权，再粘贴确认页 URL。
					for _, line := range strings.Split(string(p), "\n") {
						if !strings.HasPrefix(line, provider.URL+"/authorize?") {
							continue
						}
						var err error
						authorizationURL, err = url.Parse(strings.TrimSpace(line))
						if err != nil {
							t.Fatal(err)
						}
						state := authorizationURL.Query().Get("state")
						if mode == "wrong state" {
							state = "wrong"
						}
						callback := NativeRedirectURI + "?code=test-code&state=" + url.QueryEscape(state)
						input.WriteString(provider.URL + "/core/redirect/confirm/?next=" + url.QueryEscape(callback) + "\n")
					}
					return instructions.Write(p)
				}),
			}
			if mode == "opener absent" {
				flow.OpenBrowser = nil
			}
			token, err := flow.Login(context.Background(), provider.URL)
			if mode == "wrong state" {
				if err == nil || !strings.Contains(err.Error(), "state") || exchanges != 0 {
					t.Fatalf("invalid state was not rejected before exchange: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if exchanges != 1 || token.AccessToken != "test-access" {
				t.Fatal("authorization did not complete")
			}
			for _, want := range []string{"another computer", "do not select Confirm", "OAuth callback URL:"} {
				if !strings.Contains(instructions.String(), want) {
					t.Errorf("instructions missing %q", want)
				}
			}
			if strings.Contains(instructions.String(), "Opening") || strings.Contains(instructions.String(), token.AccessToken) {
				t.Fatal("misleading or sensitive instructions")
			}
		})
	}
}

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
