package guiconfig

import (
	"path/filepath"
	"testing"
)

func TestStoreReturnsDefaultsWhenMissing(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := Default()
	if got != want {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}

func TestStoreRoundTripsDesktopPreferences(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "nested", "gui.toml")}
	want := Default()
	want.Appearance.Theme = "dark"
	want.Appearance.TerminalFontFamily = "Cascadia Mono"
	want.Appearance.TerminalFontSize = 16
	want.Behavior.ConfirmCloseActiveSession = false
	want.Window = WindowPlacement{HasBounds: true, Maximized: true, X: -1200, Y: 80, Width: 1100, Height: 700}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got != want {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
}
