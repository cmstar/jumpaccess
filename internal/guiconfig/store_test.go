package guiconfig

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStoreUpdateSerializesWorkspaceAndWindowChanges(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.Update(context.Background(), func(value *Config) error {
			close(firstStarted)
			<-releaseFirst
			value.Workspace = Workspace{ActiveTabID: "assets", Tabs: []WorkspaceTab{{ID: "assets", Type: "assets"}}}
			return nil
		})
	}()
	<-firstStarted

	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- store.Update(context.Background(), func(value *Config) error {
			close(secondStarted)
			value.Window = WindowPlacement{HasBounds: true, X: 120, Y: 90, Width: 1440, Height: 900}
			return nil
		})
	}()
	select {
	case <-secondStarted:
		t.Fatal("second update started before first update released the GUI config lock")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace.ActiveTabID != "assets" || !got.Window.HasBounds {
		t.Fatalf("Load = %#v, want both workspace and window updates", got)
	}
}

func TestStoreReturnsDefaultsWhenMissing(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := Default()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}

func TestStoreRoundTripsDesktopPreferences(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "nested", "gui.toml")}
	want := Default()
	want.Appearance.Theme = "dark"
	want.Terminal.FontFamily = "Cascadia Mono"
	want.Terminal.FontSize = 16
	want.Terminal.ColorScheme = "catppuccin-latte"
	want.Terminal.LineHeight = 1.25
	want.Terminal.CursorStyle = "quarter_block"
	want.Terminal.CursorBlink = false
	want.Terminal.RightClickAction = TerminalRightClickContextMenu
	want.Terminal.WarnOnMultiLinePaste = false
	want.Tabs.ConfirmCloseActiveSession = false
	want.Workspace = Workspace{
		ActiveTabID: "ssh-1",
		Tabs: []WorkspaceTab{
			{ID: "assets", Type: "assets"},
			{ID: "ssh-1", Type: "ssh", Profile: "production", Organization: "org-1", Target: "production-web", Account: "account-1", AssetID: "asset-1", AssetName: "prod-web-01", Alias: "production-web"},
		},
	}
	want.Window = WindowPlacement{HasBounds: true, Maximized: true, Display: `\\.\DISPLAY2`, X: 720, Y: 80, Width: 1100, Height: 700}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	for _, field := range []string{"line_height = 1.25", "cursor_style = \"quarter_block\"", "cursor_blink = false"} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("missing terminal style field %q", field)
		}
	}
	if !strings.Contains(encoded, "[terminal]") || !strings.Contains(encoded, "right_click_action = \"context_menu\"") || !strings.Contains(encoded, "warn_on_multi_line_paste = false") || !strings.Contains(encoded, "[tabs]") {
		t.Fatalf("encoded GUI config does not use terminal preference groups:\n%s", encoded)
	}
	if strings.Contains(encoded, "[behavior]") || strings.Contains(encoded, "terminal_font_family") {
		t.Fatalf("encoded GUI config still contains legacy preference groups:\n%s", encoded)
	}
}

func TestStoreLoadsLegacyTerminalStyleWithoutRewritingFile(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	original := []byte("version = 5\n[terminal]\ncolor_scheme = \"dracula\"\nwarn_on_multi_line_paste = false\n")
	if err := os.WriteFile(store.Path, original, 0600); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != CurrentVersion || value.Terminal.LineHeight != 1 || !value.Terminal.CursorBlink || value.Terminal.CursorStyle != "block" || value.Terminal.ColorScheme != "dracula" || value.Terminal.WarnOnMultiLinePaste {
		t.Fatalf("unexpected upgraded config: %#v", value.Terminal)
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("loading legacy GUI config rewrote the file")
	}
}
