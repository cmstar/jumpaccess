package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

func TestFullscreenPreservesPlacementOnQuitAndRestore(t *testing.T) {
	for _, maximized := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "maximized"}[maximized], func(t *testing.T) {
			prefs := guiconfig.Default()
			prefs.Window = guiconfig.WindowPlacement{HasBounds: true, Display: "primary", X: 120, Y: 90, Width: 1100, Height: 700, Maximized: maximized}
			store := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
			if err := store.Save(prefs); err != nil {
				t.Fatal(err)
			}
			window := &fakeFullscreenWindow{fakeDesktopWindow: fakeDesktopWindow{x: 120, y: 90, width: 1100, height: 700, normal: !maximized, maximized: maximized}}
			app := &desktopApp{ctx: context.Background(), preferences: store, initialPreferences: prefs, window: window, goos: "windows", displayAreas: func(context.Context) ([]displayArea, error) { return dualDisplays, nil }}
			if err := app.SetWindowFullscreen(true); err != nil {
				t.Fatal(err)
			}
			if !window.fullscreen {
				t.Fatal("未进入原生全屏")
			}
			if err := app.SetWindowFullscreen(true); err != nil {
				t.Fatal(err)
			}
			if err := app.saveWindowPlacement(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if got.Window != prefs.Window {
				t.Fatalf("全屏退出程序污染窗口状态: %#v", got.Window)
			}
			if err := app.SetWindowFullscreen(false); err != nil {
				t.Fatal(err)
			}
			if window.fullscreen || window.maximized != maximized {
				t.Fatalf("未恢复原窗口状态: %#v", window)
			}
		})
	}
}

type fakeFullscreenWindow struct {
	fakeDesktopWindow
	fullscreen bool
	previous   fakeDesktopWindow
}

func (w *fakeFullscreenWindow) IsFullscreen(context.Context) bool { return w.fullscreen }
func (w *fakeFullscreenWindow) Fullscreen(context.Context) {
	w.previous = w.fakeDesktopWindow
	w.fullscreen, w.normal = true, false
	w.x, w.y, w.width, w.height = 0, 0, 1920, 1080
}
func (w *fakeFullscreenWindow) Unfullscreen(context.Context) {
	w.fullscreen = false
	w.fakeDesktopWindow = w.previous
}

func TestMacFullscreenKeepsEscapeForTerminal(t *testing.T) {
	options := newWailsOptions(&desktopApp{initialPreferences: guiconfig.Default()})
	configureWindowChrome(options, "darwin")
	if !options.Mac.DisableEscapeExitsFullscreen {
		t.Fatal("Esc 应交给终端及弹窗")
	}
}

func TestFullscreenRestoresVisibleWindowWhenDisplayIsRemoved(t *testing.T) {
	for _, maximized := range []bool{false, true} {
		window := &fakeFullscreenWindow{fakeDesktopWindow: fakeDesktopWindow{x: -1700, y: 100, width: 1100, height: 700, normal: !maximized, maximized: maximized}}
		prefs := guiconfig.Default()
		prefs.Window = guiconfig.WindowPlacement{HasBounds: true, Display: "left", X: 220, Y: 100, Width: 1100, Height: 700, Maximized: maximized}
		displays := dualDisplays
		app := &desktopApp{ctx: context.Background(), window: window, initialPreferences: prefs, goos: "windows", displayAreas: func(context.Context) ([]displayArea, error) { return displays, nil }}
		if err := app.SetWindowFullscreen(true); err != nil {
			t.Fatal(err)
		}
		displays = dualDisplays[:1]
		if err := app.SetWindowFullscreen(false); err != nil {
			t.Fatal(err)
		}
		if !window.positionSet || window.setX != 410 || window.setY != 170 {
			t.Fatalf("未恢复到主屏可见区域: %#v", window)
		}
		if window.maximized != maximized {
			t.Fatal("最大化状态未保留")
		}
	}
}
