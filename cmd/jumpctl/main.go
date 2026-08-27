package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cmstar/jumpaccess/internal/appdir"
	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/cli"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/oauth"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"github.com/cmstar/jumpaccess/internal/sshhostkey"
	"github.com/cmstar/jumpaccess/internal/systemopen"
	"github.com/cmstar/jumpaccess/internal/terminalprompt"
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
	connectionService := connectapp.Service{
		Config: configStore,
		Tokens: manager,
		NewAPI: func(site, accessToken, organization string) (connectapp.API, error) {
			return jumpserver.NewClient(site, accessToken, organization, httpClient)
		},
	}
	command := cli.NewRoot(cli.Dependencies{
		Version:    version,
		ConfigPath: configPath,
		Store:      configStore,
		OpenFile:   systemopen.Open,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Auth:       authService,
		Connect:    connectionService,
		SelectAccount: func(accounts []jumpserver.Account) (jumpserver.Account, error) {
			return terminalprompt.SelectAccount(os.Stdin, os.Stderr, accounts)
		},
		RunSSH: func(ctx context.Context, prepared connectapp.Prepared) error {
			hostKeys := sshhostkey.Store{
				Path: filepath.Join(rootDir, "known_hosts"),
				Confirm: func(host, fingerprint string) (bool, error) {
					return terminalprompt.ConfirmHostKey(os.Stdin, os.Stderr, host, fingerprint)
				},
			}
			callback, err := hostKeys.Callback(true)
			if err != nil {
				return err
			}
			refreshContext, stopRefresh := context.WithCancel(context.Background())
			defer stopRefresh()
			go manager.Supervise(refreshContext, prepared.Selection.Profile, configuration.Behavior.RefreshCheckInterval.Duration, func(err error) {
				fmt.Fprintf(os.Stderr, "warning: OAuth refresh failed: %v\n", err)
			})
			return (sshclient.Runner{
				Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
				HostKeyCallback: callback,
				Timeout:         configuration.Behavior.ConnectTimeout.Duration,
			}).Run(ctx, prepared.Connection)
		},
	})
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
