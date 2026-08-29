package main

import (
	"context"
	"path/filepath"

	jumpaccess "github.com/cmstar/jumpaccess"
	desktopapp "github.com/cmstar/jumpaccess/internal/application/desktop"
	sshsessionapp "github.com/cmstar/jumpaccess/internal/application/sshsession"
	"github.com/cmstar/jumpaccess/internal/bootstrap"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"github.com/cmstar/jumpaccess/internal/sshhostkey"
	"github.com/cmstar/jumpaccess/internal/systemopen"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

// desktopApp 是 Wails 表现层入口，负责把共享应用服务适配为绑定方法和事件。
type desktopApp struct {
	ctx         context.Context
	core        bootstrap.Runtime
	preferences guiconfig.Store
	api         desktopapp.Service
	sessions    *sshsessionapp.Manager
	hostKeys    *desktopapp.HostKeyCoordinator
}

func newDesktopApp(rootDir string) (*desktopApp, error) {
	core, err := bootstrap.New(bootstrap.Options{RootDir: rootDir})
	if err != nil {
		return nil, err
	}
	preferences := guiconfig.Store{Path: filepath.Join(rootDir, "gui.toml")}
	if _, err := preferences.Load(); err != nil {
		return nil, err
	}
	login := &desktopapp.LoginCoordinator{
		Config:      core.Store,
		Tokens:      core.Tokens,
		HTTPClient:  core.HTTPClient,
		OpenBrowser: systemopen.Open,
		Timeout:     core.Configuration.Behavior.OAuthTimeout.Duration,
	}
	app := &desktopApp{
		core:        core,
		preferences: preferences,
		api: desktopapp.Service{
			Version:     version,
			Licenses:    jumpaccess.Licenses(),
			Login:       login,
			Config:      core.Store,
			Auth:        core.Auth,
			Resources:   core.Resources,
			Settings:    core.Settings,
			Preferences: preferences,
		},
	}
	hostKeys := &desktopapp.HostKeyCoordinator{
		Emit: func(prompt desktopapp.HostKeyPrompt) {
			runtime.EventsEmit(app.context(), "ssh:host-key", prompt)
		},
	}
	app.hostKeys = hostKeys
	app.sessions = &sshsessionapp.Manager{
		Prepare: core.Connect,
		HostKeyCallback: func(ctx context.Context) (ssh.HostKeyCallback, error) {
			return (sshhostkey.Store{
				Path: filepath.Join(rootDir, "known_hosts"),
				Confirm: func(host, fingerprint string) (bool, error) {
					return hostKeys.Confirm(ctx, host, fingerprint)
				},
			}).Callback(true)
		},
		Open: func(ctx context.Context, options sshclient.OpenOptions) (sshsessionapp.TerminalSession, error) {
			return sshclient.Open(ctx, options)
		},
		Timeout: core.Configuration.Behavior.ConnectTimeout.Duration,
		EmitState: func(event sshsessionapp.StateEvent) {
			runtime.EventsEmit(app.context(), "ssh:state", event)
		},
		EmitOutput: func(event sshsessionapp.OutputEvent) {
			runtime.EventsEmit(app.context(), "ssh:output", event)
		},
	}
	return app, nil
}

func (a *desktopApp) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *desktopApp) shutdown(context.Context) {
	_ = a.sessions.CloseAll()
}
