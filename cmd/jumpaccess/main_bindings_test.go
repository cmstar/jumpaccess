//go:build bindings

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

func TestBindingsAppCreationDoesNotReadUserData(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	root := filepath.Join(localAppData, "JumpAccess")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gui.toml"), []byte("version = 999\n[terminal]\nfuture = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := newDesktopAppForRun()
	if err != nil {
		t.Fatalf("bindings app creation read user data: %v", err)
	}
	if !reflect.DeepEqual(app.initialPreferences, guiconfig.Default()) {
		t.Fatalf("bindings preferences = %#v, want defaults", app.initialPreferences)
	}
}
