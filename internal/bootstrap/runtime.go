package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	resourcesapp "github.com/cmstar/jumpaccess/internal/application/resources"
	settingsapp "github.com/cmstar/jumpaccess/internal/application/settings"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/oauth"
)

type LoginFlow func(context.Context, string) (credential.Token, error)

type Options struct {
	RootDir         string
	LoginFlow       LoginFlow
	ManualLoginFlow LoginFlow
}

type Runtime struct {
	RootDir       string
	ConfigPath    string
	Configuration projectconfig.Config
	Store         projectconfig.Store
	HTTPClient    *http.Client
	Tokens        credential.Repository
	AuthManager   authapp.Manager
	Auth          authapp.Service
	Connect       connectapp.Service
	Resources     resourcesapp.Service
	Settings      settingsapp.Service
}

func New(options Options) (Runtime, error) {
	if options.RootDir == "" {
		return Runtime{}, fmt.Errorf("application root directory is empty")
	}
	configPath := filepath.Join(options.RootDir, "config.toml")
	store := projectconfig.Store{Path: configPath}
	configuration, err := store.Load()
	if err != nil {
		return Runtime{}, err
	}
	httpClient := &http.Client{Timeout: configuration.Behavior.ConnectTimeout.Duration}
	tokens := credential.Repository{
		Backend: credential.NewFileBackend(filepath.Join(options.RootDir, "credentials")),
	}
	refresh := func(ctx context.Context, old credential.Token) (oauth.TokenResponse, error) {
		metadata, err := oauth.Discover(ctx, httpClient, old.Site)
		if err != nil {
			return oauth.TokenResponse{}, err
		}
		return (oauth.Client{HTTPClient: httpClient, Metadata: metadata}).Refresh(ctx, old.RefreshToken)
	}
	manager := authapp.Manager{
		Tokens:        tokens,
		Locker:        filelock.Locker{Dir: filepath.Join(options.RootDir, "locks")},
		Refresh:       refresh,
		RefreshBefore: configuration.Behavior.RefreshBeforeExpiry.Duration,
	}
	revoke := func(ctx context.Context, token credential.Token) error {
		metadata, err := oauth.Discover(ctx, httpClient, token.Site)
		if err != nil {
			return err
		}
		value := token.RefreshToken
		if value == "" {
			value = token.AccessToken
		}
		return (oauth.Client{HTTPClient: httpClient, Metadata: metadata}).Revoke(ctx, value)
	}
	authService := authapp.Service{
		Config:          store,
		Tokens:          tokens,
		Manager:         manager,
		LoginFlow:       options.LoginFlow,
		ManualLoginFlow: options.ManualLoginFlow,
		Revoke:          revoke,
		Timeout:         configuration.Behavior.OAuthTimeout.Duration,
	}
	connectService := connectapp.Service{
		Config: store,
		Tokens: manager,
		NewAPI: func(site, accessToken, organization string) (connectapp.API, error) {
			return jumpserver.NewClient(site, accessToken, organization, httpClient)
		},
	}
	resourceService := resourcesapp.Service{
		Config: store,
		Tokens: manager,
		NewAPI: func(site, accessToken, organization string) (resourcesapp.API, error) {
			return jumpserver.NewClient(site, accessToken, organization, httpClient)
		},
	}
	return Runtime{
		RootDir:       options.RootDir,
		ConfigPath:    configPath,
		Configuration: configuration,
		Store:         store,
		HTTPClient:    httpClient,
		Tokens:        tokens,
		AuthManager:   manager,
		Auth:          authService,
		Connect:       connectService,
		Resources:     resourceService,
		Settings:      settingsapp.Service{Store: store, Credentials: tokens},
	}, nil
}
