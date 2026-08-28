package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
)

const discoveryPath = "/core/auth/oauth2-provider/.well-known/oauth-authorization-server"

type Metadata struct {
	Issuer                string `json:"issuer"`
	ClientID              string `json:"client_id"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

type Authorization struct {
	URL      string
	State    string
	Verifier string
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type TokenEndpointError struct {
	Code       string
	StatusCode int
}

func (e *TokenEndpointError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("OAuth token endpoint returned HTTP status %d", e.StatusCode)
	}
	return fmt.Sprintf("OAuth token endpoint returned %s (HTTP %d)", e.Code, e.StatusCode)
}

type Client struct {
	HTTPClient *http.Client
	Metadata   Metadata
}

func Discover(ctx context.Context, client *http.Client, site string) (Metadata, error) {
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := strings.TrimRight(site, "/") + discoveryPath
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Metadata{}, fmt.Errorf("create OAuth discovery request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return Metadata{}, fmt.Errorf("discover OAuth provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Metadata{}, fmt.Errorf("discover OAuth provider: unexpected HTTP status %d", response.StatusCode)
	}
	var metadata Metadata
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&metadata); err != nil {
		return Metadata{}, fmt.Errorf("decode OAuth discovery response: %w", err)
	}
	if metadata.ClientID == "" || metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		return Metadata{}, fmt.Errorf("OAuth discovery response is incomplete")
	}
	return metadata, nil
}

func NewAuthorization(metadata Metadata, redirectURI string, random io.Reader) (Authorization, error) {
	state, err := randomValue(random)
	if err != nil {
		return Authorization{}, fmt.Errorf("generate OAuth state: %w", err)
	}
	verifier, err := randomValue(random)
	if err != nil {
		return Authorization{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])

	endpoint, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return Authorization{}, fmt.Errorf("parse OAuth authorization endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("client_id", metadata.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", "write read")
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	endpoint.RawQuery = query.Encode()
	return Authorization{URL: endpoint.String(), State: state, Verifier: verifier}, nil
}

func randomValue(random io.Reader) (string, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(random, bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func ParseCallback(request *http.Request, expectedState string) (string, error) {
	query := request.URL.Query()
	if oauthError := query.Get("error"); oauthError != "" {
		return "", fmt.Errorf("OAuth authorization failed: %s", oauthError)
	}
	state := query.Get("state")
	if state == "" || state != expectedState {
		return "", fmt.Errorf("OAuth callback state did not match")
	}
	code := query.Get("code")
	if code == "" {
		return "", fmt.Errorf("OAuth callback did not include an authorization code")
	}
	return code, nil
}

// ParseCallbackURL 接受原生回调本身，或 next 参数中包含原生回调的 JumpServer 确认页 URL。
func ParseCallbackURL(rawURL, expectedRedirectURI, expectedState string) (string, error) {
	callback, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse OAuth callback URL: %w", err)
	}
	if (callback.Scheme == "http" || callback.Scheme == "https") && callback.Query().Get("next") != "" {
		callback, err = url.Parse(callback.Query().Get("next"))
		if err != nil {
			return "", fmt.Errorf("parse OAuth callback URL from confirmation page: %w", err)
		}
	}
	expected, err := url.Parse(expectedRedirectURI)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return "", fmt.Errorf("invalid expected OAuth redirect URI")
	}
	if !strings.EqualFold(callback.Scheme, expected.Scheme) ||
		!strings.EqualFold(callback.Host, expected.Host) ||
		callback.Path != expected.Path || callback.User != nil || callback.Fragment != "" {
		return "", fmt.Errorf("OAuth callback target did not match %s", expectedRedirectURI)
	}
	return ParseCallback(&http.Request{URL: callback}, expectedState)
}

func (c Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (TokenResponse, error) {
	return c.requestToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {c.Metadata.ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
}

func (c Client) Refresh(ctx context.Context, refreshToken string) (TokenResponse, error) {
	return c.requestToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.Metadata.ClientID},
		"refresh_token": {refreshToken},
	})
}

func (c Client) Revoke(ctx context.Context, token string) error {
	if c.Metadata.RevocationEndpoint == "" || token == "" {
		return nil
	}
	values := url.Values{"client_id": {c.Metadata.ClientID}, "token": {token}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Metadata.RevocationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("create OAuth revocation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return fmt.Errorf("revoke OAuth token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("revoke OAuth token: unexpected HTTP status %d", response.StatusCode)
	}
	return nil
}

func (c Client) requestToken(ctx context.Context, values url.Values) (TokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Metadata.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("create OAuth token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("request OAuth token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var payload struct {
			Code string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload)
		return TokenResponse{}, &TokenEndpointError{Code: payload.Code, StatusCode: response.StatusCode}
	}
	var token TokenResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil {
		return TokenResponse{}, fmt.Errorf("decode OAuth token response: %w", err)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 {
		return TokenResponse{}, fmt.Errorf("OAuth token response is incomplete")
	}
	return token, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (t TokenResponse) Record(clientID, site string, now time.Time) (credential.Token, error) {
	if t.AccessToken == "" || t.ExpiresIn <= 0 {
		return credential.Token{}, fmt.Errorf("OAuth token response is incomplete")
	}
	return credential.Token{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    t.TokenType,
		ClientID:     clientID,
		Site:         site,
		ExpiresAt:    now.Add(time.Duration(t.ExpiresIn) * time.Second),
		RefreshedAt:  now,
	}, nil
}
