package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

func TestDesktopStartsAndSavesWithMCPBranchConfiguration(t *testing.T) {
	root := t.TempDir()
	shared := []byte("version = 1\n[mcp]\nenabled = true\nport = 19876\n")
	gui := []byte("version = 15\n[mcp]\nauto_start = true\n")
	for name, data := range map[string][]byte{"config.toml": shared, "gui.toml": gui} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	app, err := newDesktopApp(root)
	if err != nil {
		t.Fatalf("desktop initialization failed: %v", err)
	}
	for name, original := range map[string][]byte{"config.toml": shared, "gui.toml": gui} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != string(original) {
			t.Fatal("startup rewrote configuration")
		}
	}
	preferences := guiconfig.Default()
	preferences.Terminal.FontSize = 18
	if err := app.SavePreferences(preferences); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveWorkspace(guiconfig.Workspace{ActiveTabID: "assets", Tabs: []guiconfig.WorkspaceTab{{ID: "assets", Type: "assets"}}}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version int
		MCP     struct {
			AutoStart bool `toml:"auto_start"`
		}
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "gui.toml"), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 15 || !got.MCP.AutoStart {
		t.Fatal("desktop save lost newer version or MCP preferences")
	}
}
