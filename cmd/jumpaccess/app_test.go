package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/cmstar/jumpaccess/internal/credential"
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

func TestDesktopAppDeleteProfileRemovesConfigurationAndCredential(t *testing.T) {
	app, err := newDesktopApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.core.Settings.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := app.core.Tokens.Save("work", credential.Token{AccessToken: "test-access-token"}); err != nil {
		t.Fatal(err)
	}

	if err := app.DeleteProfile("work"); err != nil {
		t.Fatalf("DeleteProfile returned error: %v", err)
	}
	configuration, err := app.core.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := configuration.Profiles["work"]; exists {
		t.Fatal("profile work still exists after DeleteProfile")
	}
	if _, err := app.core.Tokens.Load("work"); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("credential load error = %v, want ErrNotFound", err)
	}
}
