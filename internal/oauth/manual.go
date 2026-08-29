package oauth

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
)

const NativeRedirectURI = "jms://auth/callback"

// ManualFlow 读取用户粘贴的 URL 来完成原生 OAuth 回调。
// 当其他程序占用 jms scheme 或系统禁止注册私有协议时，它也是长期保留的回退方式。
type ManualFlow struct {
	HTTPClient  *http.Client
	RedirectURI string
	OpenBrowser func(string) error
	Input       io.Reader
	Output      io.Writer
	Now         func() time.Time
}

type ManualAuthorization struct {
	URL         string
	httpClient  *http.Client
	metadata    Metadata
	redirectURI string
	site        string
	state       string
	verifier    string
}

func BeginManualAuthorization(ctx context.Context, httpClient *http.Client, site, redirectURI string) (ManualAuthorization, error) {
	if redirectURI == "" {
		redirectURI = NativeRedirectURI
	}
	metadata, err := Discover(ctx, httpClient, site)
	if err != nil {
		return ManualAuthorization{}, err
	}
	authorization, err := NewAuthorization(metadata, redirectURI, rand.Reader)
	if err != nil {
		return ManualAuthorization{}, err
	}
	return ManualAuthorization{
		URL:         authorization.URL,
		httpClient:  httpClient,
		metadata:    metadata,
		redirectURI: redirectURI,
		site:        strings.TrimRight(site, "/"),
		state:       authorization.State,
		verifier:    authorization.Verifier,
	}, nil
}

func (a ManualAuthorization) Complete(ctx context.Context, rawCallback string, now time.Time) (credential.Token, error) {
	code, err := ParseCallbackURL(rawCallback, a.redirectURI, a.state)
	if err != nil {
		return credential.Token{}, err
	}
	response, err := (Client{HTTPClient: a.httpClient, Metadata: a.metadata}).Exchange(
		ctx, code, a.verifier, a.redirectURI,
	)
	if err != nil {
		return credential.Token{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	return response.Record(a.metadata.ClientID, a.site, now)
}

func (f ManualFlow) Login(ctx context.Context, site string) (credential.Token, error) {
	if f.OpenBrowser == nil {
		return credential.Token{}, fmt.Errorf("browser opener is unavailable")
	}
	if f.Input == nil {
		return credential.Token{}, fmt.Errorf("manual OAuth callback input is unavailable")
	}
	authorization, err := BeginManualAuthorization(ctx, f.HTTPClient, site, f.RedirectURI)
	if err != nil {
		return credential.Token{}, err
	}

	output := f.Output
	if output == nil {
		output = io.Discard
	}
	if _, err := fmt.Fprintf(output, `Opening the JumpServer authorization page:
%s

After authorization reaches the JumpServer confirmation page, do not select Confirm.
Copy either the jms:// callback link or the complete confirmation-page URL,
then paste it here and press Enter:
OAuth callback URL: `, authorization.URL); err != nil {
		return credential.Token{}, fmt.Errorf("write OAuth instructions: %w", err)
	}
	if err := f.OpenBrowser(authorization.URL); err != nil {
		return credential.Token{}, fmt.Errorf("open authorization URL: %w", err)
	}

	rawCallback, err := readCallbackLine(ctx, f.Input)
	if err != nil {
		return credential.Token{}, err
	}
	return authorization.Complete(ctx, rawCallback, f.now())
}

func readCallbackLine(ctx context.Context, input io.Reader) (string, error) {
	type result struct {
		value string
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		value, err := bufio.NewReader(io.LimitReader(input, 64<<10)).ReadString('\n')
		if errors.Is(err, io.EOF) && value != "" {
			err = nil
		}
		resultCh <- result{value: strings.TrimSpace(value), err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("wait for manual OAuth callback: %w", ctx.Err())
	case result := <-resultCh:
		if result.err != nil {
			return "", fmt.Errorf("read manual OAuth callback: %w", result.err)
		}
		if result.value == "" {
			return "", fmt.Errorf("manual OAuth callback URL is empty")
		}
		return result.value, nil
	}
}

func (f ManualFlow) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}
