package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/wailsapp/wails/v2/pkg/options"
)

type fakeDesktopWindow struct {
	x, y          int
	width, height int
	maximized     bool
	normal        bool
	setX, setY    int
	positionSet   bool
}

func (w *fakeDesktopWindow) GetPosition(context.Context) (int, int) { return w.x, w.y }
func (w *fakeDesktopWindow) GetSize(context.Context) (int, int)     { return w.width, w.height }
func (w *fakeDesktopWindow) IsMaximized(context.Context) bool       { return w.maximized }
func (w *fakeDesktopWindow) IsNormal(context.Context) bool          { return w.normal }
func (w *fakeDesktopWindow) SetPosition(_ context.Context, x, y int) {
	w.setX, w.setY, w.positionSet = x, y, true
}

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
	if !reflect.DeepEqual(preferences, guiconfig.Default()) {
		t.Fatalf("GUI preferences = %#v, want defaults", preferences)
	}
}

func TestWailsOptionsRestoreSavedWindowSizeAndMaximizedState(t *testing.T) {
	app := &desktopApp{initialPreferences: guiconfig.Default()}
	app.initialPreferences.Window = guiconfig.WindowPlacement{
		HasBounds: true,
		Maximized: true,
		X:         140,
		Y:         90,
		Width:     1440,
		Height:    900,
	}

	got := newWailsOptions(app)
	if got.Width != 1440 || got.Height != 900 {
		t.Fatalf("window size = %dx%d, want 1440x900", got.Width, got.Height)
	}
	if got.WindowStartState != options.Maximised {
		t.Fatalf("window start state = %v, want maximised", got.WindowStartState)
	}
}

func TestDesktopAppStartupRestoresSavedNormalWindowPosition(t *testing.T) {
	window := &fakeDesktopWindow{}
	app := &desktopApp{window: window, initialPreferences: guiconfig.Default()}
	app.initialPreferences.Window = guiconfig.WindowPlacement{
		HasBounds: true,
		X:         -1200,
		Y:         80,
		Width:     1100,
		Height:    700,
	}

	app.startup(context.Background())

	if !window.positionSet || window.setX != -1200 || window.setY != 80 {
		t.Fatalf("restored position = (%d, %d), set=%v", window.setX, window.setY, window.positionSet)
	}
}

func TestDesktopAppBeforeCloseSavesNormalWindowPlacement(t *testing.T) {
	store := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	preferences := guiconfig.Default()
	preferences.Appearance.Theme = "dark"
	if err := store.Save(preferences); err != nil {
		t.Fatal(err)
	}
	window := &fakeDesktopWindow{x: -1200, y: 80, width: 1100, height: 700, normal: true}
	app := &desktopApp{preferences: store, window: window}

	if prevent := app.beforeClose(context.Background()); prevent {
		t.Fatal("beforeClose prevented application shutdown")
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := guiconfig.WindowPlacement{HasBounds: true, X: -1200, Y: 80, Width: 1100, Height: 700}
	if got.Window != want {
		t.Fatalf("window placement = %#v, want %#v", got.Window, want)
	}
	if got.Appearance.Theme != "dark" {
		t.Fatalf("theme = %q, want preserved dark", got.Appearance.Theme)
	}
}

func TestDesktopAppBeforeCloseSavesMaximizedStateWithoutReplacingNormalBounds(t *testing.T) {
	store := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	preferences := guiconfig.Default()
	preferences.Window = guiconfig.WindowPlacement{HasBounds: true, X: 120, Y: 90, Width: 1440, Height: 900}
	if err := store.Save(preferences); err != nil {
		t.Fatal(err)
	}
	window := &fakeDesktopWindow{x: 0, y: 0, width: 1920, height: 1080, maximized: true}
	app := &desktopApp{preferences: store, window: window}

	app.beforeClose(context.Background())

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := preferences.Window
	want.Maximized = true
	if got.Window != want {
		t.Fatalf("window placement = %#v, want %#v", got.Window, want)
	}
}

func TestDesktopAppBeforeClosePreservesNormalBoundsWhenMinimized(t *testing.T) {
	store := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	preferences := guiconfig.Default()
	preferences.Window = guiconfig.WindowPlacement{HasBounds: true, X: 120, Y: 90, Width: 1440, Height: 900}
	if err := store.Save(preferences); err != nil {
		t.Fatal(err)
	}
	window := &fakeDesktopWindow{x: -32000, y: -32000, width: 160, height: 28}
	app := &desktopApp{preferences: store, window: window}

	app.beforeClose(context.Background())

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Window != preferences.Window {
		t.Fatalf("window placement = %#v, want %#v", got.Window, preferences.Window)
	}
}

func TestDesktopAppSaveWorkspacePersistsDisconnectedSSHTabWithoutStartingSession(t *testing.T) {
	app, err := newDesktopApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := guiconfig.Workspace{ActiveTabID: "ssh-1", Tabs: []guiconfig.WorkspaceTab{{
		ID: "ssh-1", Type: "ssh", Profile: "production", Organization: "org-1", Target: "asset-1", Account: "account-1", AssetID: "asset-1", AssetName: "web-01",
	}}}

	if err := app.SaveWorkspace(want); err != nil {
		t.Fatal(err)
	}
	state, err := app.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.Workspace, want) {
		t.Fatalf("workspace = %#v, want %#v", state.Workspace, want)
	}
	if sessions := app.ListSSHSessions(); len(sessions) != 0 {
		t.Fatalf("restoring workspace started SSH sessions: %#v", sessions)
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
	if err := app.SaveWorkspace(guiconfig.Workspace{ActiveTabID: "ssh-work", Tabs: []guiconfig.WorkspaceTab{
		{ID: "assets", Type: "assets"},
		{ID: "ssh-work", Type: "ssh", Profile: "work", Organization: "org-1", Target: "asset-1", Account: "account-1", AssetID: "asset-1", AssetName: "web-01"},
	}}); err != nil {
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
	preferences, err := app.preferences.Load()
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace := guiconfig.Workspace{ActiveTabID: "assets", Tabs: []guiconfig.WorkspaceTab{{ID: "assets", Type: "assets"}}}
	if !reflect.DeepEqual(preferences.Workspace, wantWorkspace) {
		t.Fatalf("workspace = %#v, want %#v", preferences.Workspace, wantWorkspace)
	}
}
