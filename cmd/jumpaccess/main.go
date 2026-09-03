package main

import (
	"embed"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

var version = "dev"

func main() {
	app, err := newDesktopAppForRun()
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
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		WindowStartState: windowStartState,
		StartHidden:      true,
		Bind: []interface{}{
			app,
		},
	}
	configureWindowChrome(result, runtime.GOOS)
	return result
}

func fail(err error) {
	reportStartupError(os.Stderr, showStartupError, err)
	os.Exit(1)
}

func reportStartupError(writer io.Writer, present func(title, message string), err error) {
	message := fmt.Sprintf("启动 JumpAccess 失败: %v", err)
	_, _ = fmt.Fprintln(writer, message)
	present("JumpAccess 无法启动", message)
}
