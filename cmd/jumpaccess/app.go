package main

import (
	"context"
	"errors"
	"path/filepath"
	stdruntime "runtime"
	"sync"
	"sync/atomic"

	jumpaccess "github.com/cmstar/jumpaccess"
	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	desktopapp "github.com/cmstar/jumpaccess/internal/application/desktop"
	sftpsessionapp "github.com/cmstar/jumpaccess/internal/application/sftpsession"
	sshsessionapp "github.com/cmstar/jumpaccess/internal/application/sshsession"
	"github.com/cmstar/jumpaccess/internal/bootstrap"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/cmstar/jumpaccess/internal/sftpclient"
	"github.com/cmstar/jumpaccess/internal/sshclient"
	"github.com/cmstar/jumpaccess/internal/sshhostkey"
	"github.com/cmstar/jumpaccess/internal/systemopen"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

// desktopApp 是 Wails 表现层入口，负责把共享应用服务适配为绑定方法和事件。
type desktopApp struct {
	ctx                  context.Context
	core                 bootstrap.Runtime
	preferences          guiconfig.Store
	initialPreferences   guiconfig.Config
	window               desktopWindow
	api                  desktopapp.Service
	sessions             *sshsessionapp.Manager
	sftp                 *sftpsessionapp.Manager
	emitEvent            func(context.Context, string, ...interface{})
	quit                 func(context.Context)
	quitConfirmed        atomic.Bool
	hostKeys             *desktopapp.HostKeyCoordinator
	superviseAuth        func(context.Context)
	stopAuthSupervision  context.CancelFunc
	goos                 string
	displayAreas         func(context.Context) ([]displayArea, error)
	windowPlacementMu    sync.Mutex
	lastNormalWindow     guiconfig.WindowPlacement
	restoreAfterMinimize *guiconfig.WindowPlacement
}

type desktopWindow interface {
	GetPosition(context.Context) (int, int)
	GetSize(context.Context) (int, int)
	IsMaximized(context.Context) bool
	IsNormal(context.Context) bool
	SetPosition(context.Context, int, int)
	SetSize(context.Context, int, int)
	Center(context.Context)
	Show(context.Context)
	Minimize(context.Context)
}

type wailsDesktopWindow struct{}

func (wailsDesktopWindow) GetPosition(ctx context.Context) (int, int) {
	return runtime.WindowGetPosition(ctx)
}

func (wailsDesktopWindow) GetSize(ctx context.Context) (int, int) {
	return runtime.WindowGetSize(ctx)
}

func (wailsDesktopWindow) IsMaximized(ctx context.Context) bool {
	return runtime.WindowIsMaximised(ctx)
}

func (wailsDesktopWindow) IsNormal(ctx context.Context) bool {
	return runtime.WindowIsNormal(ctx)
}

func (wailsDesktopWindow) SetPosition(ctx context.Context, x, y int) {
	runtime.WindowSetPosition(ctx, x, y)
}

func (wailsDesktopWindow) SetSize(ctx context.Context, width, height int) {
	runtime.WindowSetSize(ctx, width, height)
}

func (wailsDesktopWindow) Center(ctx context.Context) {
	runtime.WindowCenter(ctx)
}

func (wailsDesktopWindow) Show(ctx context.Context) {
	runtime.WindowShow(ctx)
}

func (wailsDesktopWindow) Minimize(ctx context.Context) {
	runtime.WindowMinimise(ctx)
}

func newDesktopApp(rootDir string) (*desktopApp, error) {
	core, err := bootstrap.New(bootstrap.Options{RootDir: rootDir})
	if err != nil {
		return nil, err
	}
	preferences := guiconfig.Store{Path: filepath.Join(rootDir, "gui.toml")}
	initialPreferences, err := preferences.Load()
	if err != nil {
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
		core:               core,
		preferences:        preferences,
		initialPreferences: initialPreferences,
		window:             wailsDesktopWindow{},
		goos:               stdruntime.GOOS,
		displayAreas:       nativeDisplayAreas,
		emitEvent:          runtime.EventsEmit,
		quit:               runtime.Quit,
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
	app.superviseAuth = authapp.ProfileSupervisor{
		Config:      core.Store,
		Credentials: core.Tokens,
		Freshener:   core.AuthManager,
		Interval:    core.Configuration.Behavior.RefreshCheckInterval.Duration,
		OnError: func(profile string, err error) {
			if profile == "" {
				runtime.LogWarningf(app.context(), "OAuth 自动续期检查失败: %v", err)
				return
			}
			if errors.Is(err, authapp.ErrLoginRequired) {
				runtime.LogWarningf(app.context(), "Profile %q OAuth 授权已失效，需要在 Profile 页面重新登录", profile)
				return
			}
			runtime.LogWarningf(app.context(), "Profile %q OAuth 自动续期失败: %v", profile, err)
		},
	}.Run
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
		EmitLatency: func(event sshsessionapp.LatencyEvent) {
			runtime.EventsEmit(app.context(), "ssh:latency", event)
		},
	}
	app.sftp = &sftpsessionapp.Manager{
		Prepare:         core.Connect,
		HostKeyCallback: app.sessions.HostKeyCallback,
		Timeout:         core.Configuration.Behavior.ConnectTimeout.Duration,
		Open: func(ctx context.Context, options sftpsessionapp.OpenOptions) (sftpsessionapp.RemoteClient, error) {
			var root *string
			for _, protocol := range options.Asset.Protocols {
				if protocol.Name == "sftp" {
					root = protocol.Settings.SFTPHome
					break
				}
			}
			return sftpclient.Open(ctx, sftpclient.OpenOptions{Connection: options.Connection, HostKeyCallback: options.HostKeyCallback, Timeout: options.Timeout, RootPath: root, AccountUsername: options.Account.Username})
		},
		EmitState:    func(event sftpsessionapp.StateEvent) { app.emitEvent(app.context(), "sftp:state", event) },
		EmitTransfer: func(event sftpsessionapp.TransferEvent) { app.emitEvent(app.context(), "sftp:transfer", event) },
	}
	return app, nil
}

func (a *desktopApp) startup(ctx context.Context) {
	a.ctx = ctx
	if a.stopAuthSupervision != nil {
		a.stopAuthSupervision()
	}
	if a.superviseAuth != nil {
		var supervisionContext context.Context
		supervisionContext, a.stopAuthSupervision = context.WithCancel(ctx)
		go a.superviseAuth(supervisionContext)
	}
}

func (a *desktopApp) domReady(ctx context.Context) {
	a.restoreInitialWindow(ctx)
	a.window.Show(ctx)
}

func (a *desktopApp) restoreInitialWindow(ctx context.Context) {
	placement := a.initialPreferences.Window
	if !placement.HasBounds {
		return
	}
	displays, err := a.listDisplayAreas(ctx)
	if err != nil || len(displays) == 0 {
		a.window.Center(ctx)
		return
	}
	current := a.currentWindowBounds(ctx)
	plan, ok := resolveWindowRestore(a.goos, placement, current, displays)
	if !ok {
		a.window.Center(ctx)
		return
	}
	if current.Width != plan.Normal.Width || current.Height != plan.Normal.Height {
		a.window.SetSize(ctx, plan.Normal.Width, plan.Normal.Height)
	}
	a.window.SetPosition(ctx, plan.SetX, plan.SetY)
	a.rememberLastNormal(plan.Normal)
}

func (a *desktopApp) beforeClose(ctx context.Context) bool {
	if a.sftp != nil && a.requestQuit(a.sftp.HasActiveTransfers()) {
		return true
	}
	if err := a.saveWindowPlacement(ctx); err != nil {
		runtime.LogErrorf(ctx, "保存窗口状态失败: %v", err)
	}
	return false
}

func (a *desktopApp) saveWindowPlacement(ctx context.Context) error {
	maximized := a.window.IsMaximized(ctx)
	var normalPlacement *guiconfig.WindowPlacement
	if !maximized && a.window.IsNormal(ctx) {
		placement := a.captureNormalWindowPlacement(ctx)
		normalPlacement = &placement
		a.rememberLastNormal(placement)
	} else if maximized {
		placement := a.lastNormalPlacement()
		if display, ok := a.currentDisplay(ctx); ok {
			if !placement.HasBounds {
				placement = a.initialPreferences.Window
				placement.HasBounds = true
				placement.Width = maxInt(placement.Width, guiconfig.MinimumWindowWidth)
				placement.Height = maxInt(placement.Height, guiconfig.MinimumWindowHeight)
				placement.X, placement.Y = centeredPosition(placement.Width, placement.Height, display)
			}
			if placement.HasBounds {
				placement.Display = display.ID
			}
		}
		if placement.HasBounds {
			normalPlacement = &placement
		}
	}
	return a.preferences.Update(ctx, func(preferences *guiconfig.Config) error {
		if normalPlacement != nil {
			preferences.Window = *normalPlacement
		}
		preferences.Window.Maximized = maximized
		return nil
	})
}

func (a *desktopApp) MinimizeWindow() {
	ctx := a.context()
	if a.window.IsNormal(ctx) {
		placement := a.captureNormalWindowPlacement(ctx)
		a.windowPlacementMu.Lock()
		a.lastNormalWindow = placement
		a.restoreAfterMinimize = &placement
		a.windowPlacementMu.Unlock()
	}
	a.window.Minimize(ctx)
}

func (a *desktopApp) EnsureWindowVisible() {
	ctx := a.context()
	if !a.window.IsNormal(ctx) {
		return
	}
	displays, err := a.listDisplayAreas(ctx)
	if err != nil || len(displays) == 0 {
		return
	}
	current := a.currentWindowBounds(ctx)
	a.windowPlacementMu.Lock()
	restoreTarget := a.restoreAfterMinimize
	a.restoreAfterMinimize = nil
	a.windowPlacementMu.Unlock()
	if restoreTarget != nil {
		if plan, ok := resolveWindowRestore(a.goos, *restoreTarget, current, displays); ok {
			if current.Width != plan.Normal.Width || current.Height != plan.Normal.Height {
				a.window.SetSize(ctx, plan.Normal.Width, plan.Normal.Height)
			}
			a.window.SetPosition(ctx, plan.SetX, plan.SetY)
			a.rememberLastNormal(plan.Normal)
		}
		return
	}
	placement, ok := normalizeCurrentWindowPlacement(a.goos, current, displays)
	if !ok {
		a.window.Center(ctx)
		return
	}
	plan, ok := resolveWindowRestore(a.goos, placement, current, displays)
	if !ok {
		return
	}
	if current.Width != plan.Normal.Width || current.Height != plan.Normal.Height {
		a.window.SetSize(ctx, plan.Normal.Width, plan.Normal.Height)
	}
	if plan.Normal.X != placement.X || plan.Normal.Y != placement.Y {
		a.window.SetPosition(ctx, plan.SetX, plan.SetY)
	}
	a.rememberLastNormal(plan.Normal)
}

func (a *desktopApp) captureNormalWindowPlacement(ctx context.Context) guiconfig.WindowPlacement {
	bounds := a.currentWindowBounds(ctx)
	displays, err := a.listDisplayAreas(ctx)
	if err == nil {
		if placement, ok := normalizeCurrentWindowPlacement(a.goos, bounds, displays); ok {
			return placement
		}
	}
	return guiconfig.WindowPlacement{
		HasBounds: true,
		X:         bounds.X,
		Y:         bounds.Y,
		Width:     bounds.Width,
		Height:    bounds.Height,
	}
}

func (a *desktopApp) currentDisplay(ctx context.Context) (displayArea, bool) {
	displays, err := a.listDisplayAreas(ctx)
	if err != nil {
		return displayArea{}, false
	}
	return currentDisplayForWindow(a.goos, a.currentWindowBounds(ctx), displays)
}

func (a *desktopApp) currentWindowBounds(ctx context.Context) windowBounds {
	x, y := a.window.GetPosition(ctx)
	width, height := a.window.GetSize(ctx)
	return windowBounds{X: x, Y: y, Width: width, Height: height}
}

func (a *desktopApp) listDisplayAreas(ctx context.Context) ([]displayArea, error) {
	if a.displayAreas == nil {
		return nil, nil
	}
	return a.displayAreas(ctx)
}

func (a *desktopApp) rememberLastNormal(placement guiconfig.WindowPlacement) {
	a.windowPlacementMu.Lock()
	a.lastNormalWindow = placement
	a.windowPlacementMu.Unlock()
}

func (a *desktopApp) lastNormalPlacement() guiconfig.WindowPlacement {
	a.windowPlacementMu.Lock()
	defer a.windowPlacementMu.Unlock()
	return a.lastNormalWindow
}

func (a *desktopApp) shutdown(context.Context) {
	if a.stopAuthSupervision != nil {
		a.stopAuthSupervision()
	}
	if a.sessions != nil {
		_ = a.sessions.CloseAll()
	}
	if a.sftp != nil {
		_ = a.sftp.CloseAll()
	}
}
