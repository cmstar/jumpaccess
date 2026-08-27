package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDiscoverUsesJumpServerWellKnownEndpoint(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(Metadata{
			Issuer:                "https://jump.example.test",
			ClientID:              "client-id",
			AuthorizationEndpoint: "https://jump.example.test/authorize",
			TokenEndpoint:         "https://jump.example.test/token",
			RevocationEndpoint:    "https://jump.example.test/revoke",
		})
	}))
	defer server.Close()

	got, err := Discover(context.Background(), server.Client(), server.URL+"/")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if requestedPath != "/core/auth/oauth2-provider/.well-known/oauth-authorization-server" {
		t.Fatalf("requested path = %q", requestedPath)
	}
	if got.ClientID != "client-id" || got.TokenEndpoint == "" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestNewAuthorizationBuildsPKCES256Request(t *testing.T) {
	metadata := Metadata{
		ClientID:              "client-id",
		AuthorizationEndpoint: "https://jump.example.test/authorize",
	}

	request, err := NewAuthorization(metadata, "http://127.0.0.1:14876/auth/callback", strings.NewReader(strings.Repeat("a", 96)))
	if err != nil {
		t.Fatalf("NewAuthorization returned error: %v", err)
	}
	parsed, err := url.Parse(request.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	for key, want := range map[string]string{
		"client_id":             "client-id",
		"redirect_uri":          "http://127.0.0.1:14876/auth/callback",
		"response_type":         "code",
		"scope":                 "write read",
		"state":                 request.State,
		"code_challenge_method": "S256",
	} {
		if got := query.Get(key); got != want {
			t.Fatalf("query[%q] = %q, want %q", key, got, want)
		}
	}
	if request.Verifier == "" || query.Get("code_challenge") == "" || query.Get("code_challenge") == request.Verifier {
		t.Fatalf("invalid PKCE request: %#v", request)
	}
}

func TestParseCallbackRequiresExactStateAndCode(t *testing.T) {
	for name, rawURL := range map[string]string{
		"missing state": "http://127.0.0.1/auth/callback?code=ok",
		"wrong state":   "http://127.0.0.1/auth/callback?code=ok&state=wrong",
		"missing code":  "http://127.0.0.1/auth/callback?state=expected",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, rawURL, nil)
			if _, err := ParseCallback(req, "expected"); err == nil {
				t.Fatal("ParseCallback unexpectedly succeeded")
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/auth/callback?code=ok&state=expected", nil)
	code, err := ParseCallback(req, "expected")
	if err != nil || code != "ok" {
		t.Fatalf("ParseCallback = %q, %v", code, err)
	}
}

func TestExchangeAndRefreshUseExpectedForms(t *testing.T) {
	requests := make(chan url.Values, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		requests <- values
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			TokenType:    "Bearer",
		})
	}))
	defer server.Close()
	client := Client{HTTPClient: server.Client(), Metadata: Metadata{ClientID: "cid", TokenEndpoint: server.URL}}

	exchanged, err := client.Exchange(context.Background(), "code", "verifier", "http://127.0.0.1/callback")
	if err != nil {
		t.Fatal(err)
	}
	if exchanged.AccessToken != "new-access" {
		t.Fatalf("exchange result = %#v", exchanged)
	}
	exchangeForm := <-requests
	if exchangeForm.Get("grant_type") != "authorization_code" || exchangeForm.Get("code_verifier") != "verifier" {
		t.Fatalf("exchange form = %#v", exchangeForm)
	}

	_, err = client.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	refreshForm := <-requests
	if refreshForm.Get("grant_type") != "refresh_token" || refreshForm.Get("refresh_token") != "old-refresh" {
		t.Fatalf("refresh form = %#v", refreshForm)
	}
}

func TestTokenResponseRecordUsesAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	record, err := (TokenResponse{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 90}).Record("client", "https://jump.example.test", now)
	if err != nil {
		t.Fatal(err)
	}
	if !record.ExpiresAt.Equal(now.Add(90*time.Second)) || !record.RefreshedAt.Equal(now) {
		t.Fatalf("record = %#v", record)
	}
}

func TestTokenEndpointErrorExposesOnlyOAuthErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"contains provider details"}`)
	}))
	defer server.Close()
	client := Client{HTTPClient: server.Client(), Metadata: Metadata{ClientID: "cid", TokenEndpoint: server.URL}}

	_, err := client.Refresh(context.Background(), "secret-refresh-token")
	var endpointError *TokenEndpointError
	if !errors.As(err, &endpointError) || endpointError.Code != "invalid_grant" {
		t.Fatalf("error = %#v", err)
	}
	if strings.Contains(err.Error(), "provider details") || strings.Contains(err.Error(), "secret-refresh-token") {
		t.Fatalf("error exposed sensitive details: %v", err)
	}
}
