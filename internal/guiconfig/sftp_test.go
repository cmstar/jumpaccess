package guiconfig

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStoreRestoresIndependentSFTPTabs(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	value := Default()
	value.Workspace = Workspace{ActiveTabID: "sftp-2", Tabs: []WorkspaceTab{
		{ID: "ssh-1", Type: "ssh", Profile: "work", Organization: "org-1", Target: "asset-1", Account: "deploy", AssetID: "asset-1", AssetName: "web"},
		{ID: "sftp-1", Type: "sftp", Profile: "work", Organization: "org-1", Target: "asset-1", Account: "deploy", AssetID: "asset-1", AssetName: "web"},
		{ID: "sftp-2", Type: "sftp", Profile: "work", Organization: "org-1", Target: "asset-1", Account: "deploy", AssetID: "asset-1", AssetName: "web"},
	}}
	if err := store.Update(context.Background(), func(saved *Config) error { *saved = value; return nil }); err != nil {
		t.Fatalf("save SFTP workspace: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Workspace, value.Workspace) {
		t.Fatalf("restored workspace = %#v, want %#v", loaded.Workspace, value.Workspace)
	}
}

func TestSFTPWorkspaceUsesNewSchemaWithoutLosingV6Preferences(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	if err := os.WriteFile(store.Path, []byte("version = 6\n[terminal]\nfont_family = 'Menlo'\nfont_size = 15\ncursor_blink = false\nwarn_on_multi_line_paste = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(saved *Config) error {
		saved.Workspace = Workspace{ActiveTabID: "files", Tabs: []WorkspaceTab{{ID: "files", Type: "sftp", Profile: "work", Organization: "org", Target: "asset", Account: "deploy", AssetID: "asset", AssetName: "web"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "version = 7") {
		t.Fatalf("saved schema does not distinguish SFTP workspace: %s", raw)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Terminal.FontFamily != "Menlo" || loaded.Terminal.FontSize != 15 || loaded.Terminal.CursorBlink || loaded.Terminal.WarnOnMultiLinePaste {
		t.Fatalf("v6 preferences lost during upgrade: %#v", loaded.Terminal)
	}
}
