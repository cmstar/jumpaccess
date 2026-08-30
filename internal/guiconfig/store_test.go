package guiconfig

import (
	"context"
	"path/filepath"
	"reflect"
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
	want.Appearance.TerminalFontFamily = "Cascadia Mono"
	want.Appearance.TerminalFontSize = 16
	want.Behavior.ConfirmCloseActiveSession = false
	want.Workspace = Workspace{
		ActiveTabID: "ssh-1",
		Tabs: []WorkspaceTab{
			{ID: "assets", Type: "assets"},
			{ID: "ssh-1", Type: "ssh", Profile: "production", Organization: "org-1", Target: "production-web", Account: "account-1", AssetID: "asset-1", AssetName: "prod-web-01", Alias: "production-web"},
		},
	}
	want.Window = WindowPlacement{HasBounds: true, Maximized: true, X: -1200, Y: 80, Width: 1100, Height: 700}

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
}
