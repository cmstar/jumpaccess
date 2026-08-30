package main

import (
	"embed"
	"fmt"
	"os"
	"runtime"

	"github.com/cmstar/jumpaccess/internal/appdir"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

var version = "dev"

func main() {
	rootDir, err := appdir.Root()
	if err != nil {
		fail(err)
		return
	}
	app, err := newDesktopApp(rootDir)
	if err != nil {
		fail(err)
		return
	}
	err = wails.Run(newWailsOptions(app))
	if err != nil {
		fail(err)
	}
}

func newWailsOptions(app *desktopApp) *options.App {
	placement := app.initialPreferences.Window
	windowStartState := options.Normal
	if placement.Maximized {
		windowStartState = options.Maximised
	}
	result := &options.App{
		Title:     "JumpAccess",
		Width:     placement.Width,
		Height:    placement.Height,
		MinWidth:  guiconfig.MinimumWindowWidth,
		MinHeight: guiconfig.MinimumWindowHeight,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 249, A: 1},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		WindowStartState: windowStartState,
		Bind: []interface{}{
			app,
		},
	}
	configureWindowChrome(result, runtime.GOOS)
	return result
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "启动 JumpAccess 失败: %v\n", err)
	os.Exit(1)
}
