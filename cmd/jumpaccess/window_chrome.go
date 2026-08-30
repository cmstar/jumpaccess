package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	macoptions "github.com/wailsapp/wails/v2/pkg/options/mac"
	windowsoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

func configureWindowChrome(value *options.App, goos string) {
	value.Frameless = goos == "windows"
	value.Windows = &windowsoptions.Options{
		DisableFramelessWindowDecorations: false,
	}
	value.Mac = &macoptions.Options{
		TitleBar: macoptions.TitleBarHiddenInset(),
	}
}
