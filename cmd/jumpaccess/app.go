package main

import (
	"context"
	"path/filepath"

	jumpaccess "github.com/cmstar/jumpaccess"
	desktopapp "github.com/cmstar/jumpaccess/internal/application/desktop"
	"github.com/cmstar/jumpaccess/internal/bootstrap"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

// desktopApp 是 Wails 表现层入口。共享应用服务会在后续步骤注入此处。
type desktopApp struct {
	ctx         context.Context
	core        bootstrap.Runtime
	preferences guiconfig.Store
	api         desktopapp.Service
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
	return &desktopApp{
		core:        core,
		preferences: preferences,
		api: desktopapp.Service{
			Version:     version,
			Licenses:    jumpaccess.Licenses(),
			Config:      core.Store,
			Auth:        core.Auth,
			Resources:   core.Resources,
			Settings:    core.Settings,
			Preferences: preferences,
		},
	}, nil
}

func (a *desktopApp) startup(ctx context.Context) {
	a.ctx = ctx
}
