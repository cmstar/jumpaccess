package oauth

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
)

type BrowserFlow struct {
	HTTPClient  *http.Client
	CallbackURL string
	OpenBrowser func(string) error
	Now         func() time.Time
}

type callbackResult struct {
	code string
	err  error
}

func (f BrowserFlow) Login(ctx context.Context, site string) (credential.Token, error) {
	if f.OpenBrowser == nil {
		return credential.Token{}, fmt.Errorf("browser opener is unavailable")
	}
	callbackURL, err := url.Parse(f.CallbackURL)
	if err != nil || callbackURL.Scheme != "http" || callbackURL.Hostname() == "" {
		return credential.Token{}, fmt.Errorf("invalid OAuth callback URL")
	}
	callbackIP := net.ParseIP(callbackURL.Hostname())
	if callbackIP == nil || !callbackIP.IsLoopback() {
		return credential.Token{}, fmt.Errorf("OAuth callback must use a loopback address")
	}
	listener, err := net.Listen("tcp", callbackURL.Host)
	if err != nil {
		return credential.Token{}, fmt.Errorf("listen for OAuth callback: %w", err)
	}
	defer listener.Close()
	if strings.HasSuffix(callbackURL.Host, ":0") {
		callbackURL.Host = listener.Addr().String()
	}

	metadata, err := Discover(ctx, f.HTTPClient, site)
	if err != nil {
		return credential.Token{}, err
	}
	authorization, err := NewAuthorization(metadata, callbackURL.String(), rand.Reader)
	if err != nil {
		return credential.Token{}, err
	}

	result := make(chan callbackResult, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc(callbackURL.Path, func(w http.ResponseWriter, request *http.Request) {
		code, parseErr := ParseCallback(request, authorization.State)
		once.Do(func() { result <- callbackResult{code: code, err: parseErr} })
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if parseErr != nil {
			http.Error(w, "JumpAccess authorization failed. Return to the terminal.", http.StatusBadRequest)
			return
		}
		_, _ = fmt.Fprintln(w, "JumpAccess authorization completed. You can close this window.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	if err := f.OpenBrowser(authorization.URL); err != nil {
		return credential.Token{}, fmt.Errorf("open authorization URL: %w", err)
	}

	var callback callbackResult
	select {
	case callback = <-result:
		if callback.err != nil {
			return credential.Token{}, callback.err
		}
	case <-ctx.Done():
		return credential.Token{}, fmt.Errorf("wait for OAuth callback: %w", ctx.Err())
	}

	response, err := (Client{HTTPClient: f.HTTPClient, Metadata: metadata}).Exchange(ctx, callback.code, authorization.Verifier, callbackURL.String())
	if err != nil {
		return credential.Token{}, err
	}
	return response.Record(metadata.ClientID, strings.TrimRight(site, "/"), f.now())
}

func (f BrowserFlow) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}
