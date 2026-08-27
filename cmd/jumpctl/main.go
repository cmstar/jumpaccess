package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cmstar/jumpaccess/internal/appdir"
	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/cli"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
	"github.com/cmstar/jumpaccess/internal/oauth"
	"github.com/cmstar/jumpaccess/internal/systemopen"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	rootDir, err := appdir.Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	configPath := filepath.Join(rootDir, "config.toml")
	configStore := projectconfig.Store{Path: configPath}
	configuration, err := configStore.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	httpClient := &http.Client{Timeout: configuration.Behavior.ConnectTimeout.Duration}
	tokenRepository := credential.Repository{Backend: credential.NewNativeBackend()}
	refresh := func(ctx context.Context, old credential.Token) (oauth.TokenResponse, error) {
		metadata, err := oauth.Discover(ctx, httpClient, old.Site)
		if err != nil {
			return oauth.TokenResponse{}, err
		}
		return (oauth.Client{HTTPClient: httpClient, Metadata: metadata}).Refresh(ctx, old.RefreshToken)
	}
	manager := authapp.Manager{
		Tokens:        tokenRepository,
		Locker:        filelock.Locker{Dir: filepath.Join(rootDir, "locks")},
		Refresh:       refresh,
		RefreshBefore: configuration.Behavior.RefreshBeforeExpiry.Duration,
	}
	authService := authapp.Service{
		Config:  configStore,
		Tokens:  tokenRepository,
		Manager: manager,
		LoginFlow: (oauth.BrowserFlow{
			HTTPClient:  httpClient,
			CallbackURL: "http://127.0.0.1:14876/auth/callback",
			OpenBrowser: systemopen.Open,
		}).Login,
		Revoke: func(ctx context.Context, token credential.Token) error {
			metadata, err := oauth.Discover(ctx, httpClient, token.Site)
			if err != nil {
				return err
			}
			value := token.RefreshToken
			if value == "" {
				value = token.AccessToken
			}
			return (oauth.Client{HTTPClient: httpClient, Metadata: metadata}).Revoke(ctx, value)
		},
		Timeout: configuration.Behavior.OAuthTimeout.Duration,
	}
	command := cli.NewRoot(cli.Dependencies{
		Version:    version,
		ConfigPath: configPath,
		Store:      configStore,
		OpenFile:   systemopen.Open,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Auth:       authService,
	})
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
