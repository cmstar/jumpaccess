package main

import (
	"path/filepath"
	"testing"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

func TestNewDesktopAppUsesSharedAndGUIConfigFiles(t *testing.T) {
	root := t.TempDir()
	app, err := newDesktopApp(root)
	if err != nil {
		t.Fatalf("newDesktopApp returned error: %v", err)
	}
	if app.core.ConfigPath != filepath.Join(root, "config.toml") {
		t.Fatalf("config path = %q", app.core.ConfigPath)
	}
	if app.preferences.Path != filepath.Join(root, "gui.toml") {
		t.Fatalf("GUI config path = %q", app.preferences.Path)
	}
	preferences, err := app.preferences.Load()
	if err != nil {
		t.Fatal(err)
	}
	if preferences != guiconfig.Default() {
		t.Fatalf("GUI preferences = %#v, want defaults", preferences)
	}
}
