//go:build !bindings

package main

import "github.com/cmstar/jumpaccess/internal/appdir"

func newDesktopAppForRun() (*desktopApp, error) {
	rootDir, err := appdir.Root()
	if err != nil {
		return nil, err
	}
	return newDesktopApp(rootDir)
}
