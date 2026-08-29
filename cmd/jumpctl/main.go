package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	jumpaccess "github.com/cmstar/jumpaccess"
	"github.com/cmstar/jumpaccess/internal/appdir"
	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/bootstrap"
	"github.com/cmstar/jumpaccess/internal/cli"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/oauth"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"github.com/cmstar/jumpaccess/internal/sshhostkey"
	"github.com/cmstar/jumpaccess/internal/sshproxy"
	"github.com/cmstar/jumpaccess/internal/sshupstream"
	"github.com/cmstar/jumpaccess/internal/stdioconn"
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
	core, err := bootstrap.New(bootstrap.Options{RootDir: rootDir})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	configuration := core.Configuration
	nativeCredentials := credential.NewNativeBackend()
	manager := core.AuthManager
	authService := core.Auth
	authService.ManualLoginFlow = (oauth.ManualFlow{
		HTTPClient:  core.HTTPClient,
		RedirectURI: oauth.NativeRedirectURI,
		OpenBrowser: systemopen.Open,
		Input:       os.Stdin,
		Output:      os.Stderr,
	}).Login
	command := cli.NewRoot(cli.Dependencies{
		Version:     version,
		Licenses:    jumpaccess.Licenses(),
		ConfigPath:  core.ConfigPath,
		Store:       core.Store,
		OpenFile:    systemopen.Open,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Credentials: core.Tokens,
		Auth:        authService,
		Connect:     core.Connect,
		Resources:   core.Resources,
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
		RunProxy: func(ctx context.Context, prepared connectapp.Prepared) error {
			callback, err := (sshhostkey.Store{Path: filepath.Join(rootDir, "known_hosts")}).Callback(false)
			if err != nil {
				return err
			}
			upstream, err := (sshupstream.Dialer{
				HostKeyCallback: callback,
				Timeout:         configuration.Behavior.ConnectTimeout.Duration,
			}).Dial(ctx, prepared.Connection)
			if err != nil {
				return err
			}
			defer upstream.Close()
			signer, err := (sshhostkey.SignerStore{Backend: nativeCredentials}).LoadOrCreate()
			if err != nil {
				return err
			}
			refreshContext, stopRefresh := context.WithCancel(context.Background())
			defer stopRefresh()
			go manager.Supervise(refreshContext, prepared.Selection.Profile, configuration.Behavior.RefreshCheckInterval.Duration, func(err error) {
				fmt.Fprintf(os.Stderr, "warning: OAuth refresh failed: %v\n", err)
			})
			transport := stdioconn.New(os.Stdin, os.Stdout, nil)
			return (sshproxy.Server{}).Run(ctx, transport, signer, upstream)
		},
	})
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
