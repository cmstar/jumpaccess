package guiconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestStorePreservesFuturePreferencesAndTabFieldsByID(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	raw := fmt.Sprintf(`version = %d
[mcp]
auto_start = false
[terminal]
future_option = 0
[workspace]
active_tab_id = "assets"
[[workspace.tabs]]
id = "assets"
type = "assets"
future = "assets-value"
[[workspace.tabs]]
id = "settings"
type = "settings"
future = "settings-value"
[[workspace.tabs]]
id = "remove"
type = "profiles"
future = "remove-value"
`, CurrentVersion+1)
	if err := os.WriteFile(store.Path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(value *Config) error {
		value.Version = CurrentVersion
		value.Terminal.FontSize = 18
		value.Workspace.Tabs = []WorkspaceTab{value.Workspace.Tabs[1], value.Workspace.Tabs[0]}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version int
		MCP     struct {
			AutoStart bool `toml:"auto_start"`
		}
		Terminal struct {
			FontSize     int `toml:"font_size"`
			FutureOption int `toml:"future_option"`
		}
		Workspace struct {
			Tabs []struct {
				ID     string
				Future string
			}
		}
	}
	metadata, err := toml.DecodeFile(store.Path, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion+1 || got.MCP.AutoStart || !metadata.IsDefined("mcp", "auto_start") || !metadata.IsDefined("terminal", "future_option") || got.Terminal.FontSize != 18 {
		t.Fatal("future preferences lost or known preference not updated")
	}
	if len(got.Workspace.Tabs) != 2 || got.Workspace.Tabs[0].ID != "settings" || got.Workspace.Tabs[0].Future != "settings-value" || got.Workspace.Tabs[1].Future != "assets-value" {
		t.Fatal("tab extensions attached to wrong tab or deleted tab resurrected")
	}
}

func TestStorePreservesInlineTabExtensions(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	raw := `workspace = { active_tab_id = "assets", tabs = [{id = "assets", type = "assets", future = 7}] }`
	if err := os.WriteFile(store.Path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(value *Config) error { value.Appearance.Theme = "dark"; return nil }); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Workspace struct{ Tabs []struct{ Future int } }
	}
	if _, err := toml.DecodeFile(store.Path, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Workspace.Tabs) != 1 || got.Workspace.Tabs[0].Future != 7 {
		t.Fatal("inline tab extension lost")
	}
}

func TestStoreMigratesLegacyKnownFieldsAndPreservesExtensions(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	raw := "version = 1\n[appearance]\nterminal_font_size = 18\nfuture = true\n[behavior]\nconfirm_close_active_session = false\nfuture = 7\n"
	if err := os.WriteFile(store.Path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(*Config) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	metadata, err := toml.DecodeFile(store.Path, &fields)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.IsDefined("appearance", "terminal_font_size") || metadata.IsDefined("behavior", "confirm_close_active_session") {
		t.Fatal("migrated keys left in old groups")
	}
	if fields["appearance"].(map[string]any)["future"] != true || fields["behavior"].(map[string]any)["future"] != int64(7) {
		t.Fatal("legacy extensions lost")
	}
	got, err := store.Load()
	if err != nil || got.Terminal.FontSize != 18 || got.Tabs.ConfirmCloseActiveSession {
		t.Fatal("legacy settings not migrated")
	}
}
