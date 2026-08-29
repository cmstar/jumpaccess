package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	app := newDesktopApp()
	err := wails.Run(&options.App{
		Title:     "JumpAccess",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 249, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "启动 JumpAccess 失败: %v\n", err)
		os.Exit(1)
	}
}
