package main

import (
	"testing"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

var dualDisplays = []displayArea{
	{ID: "primary", X: 0, Y: 0, Width: 1920, Height: 1040, Primary: true},
	{ID: "left", X: -1920, Y: 0, Width: 1920, Height: 1040},
}

func TestResolveWindowRestoreMovesFromCurrentDisplayToSavedDisplayOnWindows(t *testing.T) {
	saved := guiconfig.WindowPlacement{HasBounds: true, Display: "primary", X: 120, Y: 90, Width: 1100, Height: 700}
	current := windowBounds{X: -1600, Y: 100, Width: 1100, Height: 700}

	plan, ok := resolveWindowRestore("windows", saved, current, dualDisplays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	// Wails 2.14 的 Windows SetPosition 参数相对当前显示器工作区；
	// 当前窗口位于左屏，因此需要跨过 1920 像素才能回到主屏的 (120, 90)。
	if plan.SetX != 2040 || plan.SetY != 90 {
		t.Fatalf("SetPosition = (%d, %d), want (2040, 90)", plan.SetX, plan.SetY)
	}
	if plan.Normal.Display != "primary" || plan.Normal.X != 120 || plan.Normal.Y != 90 {
		t.Fatalf("normalized placement = %#v", plan.Normal)
	}
}

func TestResolveWindowRestoreKeepsLegacyAbsoluteWindowsPosition(t *testing.T) {
	saved := guiconfig.WindowPlacement{HasBounds: true, X: -1200, Y: 80, Width: 1100, Height: 700}
	current := windowBounds{X: 300, Y: 100, Width: 1100, Height: 700}

	plan, ok := resolveWindowRestore("windows", saved, current, dualDisplays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	if plan.SetX != -1200 || plan.SetY != 80 {
		t.Fatalf("SetPosition = (%d, %d), want legacy absolute (-1200, 80)", plan.SetX, plan.SetY)
	}
	if plan.Normal.Display != "left" || plan.Normal.X != 720 || plan.Normal.Y != 80 {
		t.Fatalf("normalized placement = %#v, want left display relative coordinates", plan.Normal)
	}
}

func TestResolveWindowRestoreCentersOnPrimaryWhenSavedDisplayIsMissing(t *testing.T) {
	saved := guiconfig.WindowPlacement{HasBounds: true, Display: "removed", X: 300, Y: 200, Width: 1100, Height: 700}
	current := windowBounds{X: -1600, Y: 100, Width: 1100, Height: 700}

	plan, ok := resolveWindowRestore("windows", saved, current, dualDisplays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	if plan.SetX != 2330 || plan.SetY != 170 {
		t.Fatalf("SetPosition = (%d, %d), want primary-centered (2330, 170) relative to current left display", plan.SetX, plan.SetY)
	}
	if plan.Normal.Display != "primary" || plan.Normal.X != 410 || plan.Normal.Y != 170 {
		t.Fatalf("normalized placement = %#v", plan.Normal)
	}
}

func TestResolveWindowRestoreClampsSavedBoundsAfterResolutionChange(t *testing.T) {
	displays := []displayArea{{ID: "primary", X: 0, Y: 0, Width: 1280, Height: 720, Primary: true, Current: true}}
	saved := guiconfig.WindowPlacement{HasBounds: true, Display: "primary", X: 600, Y: 300, Width: 1100, Height: 700}

	plan, ok := resolveWindowRestore("windows", saved, windowBounds{X: 0, Y: 0, Width: 1100, Height: 700}, displays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	if plan.SetX != 180 || plan.SetY != 20 || plan.Normal.X != 180 || plan.Normal.Y != 20 {
		t.Fatalf("clamped plan = %#v, want relative position (180, 20)", plan)
	}
}

func TestResolveWindowRestoreShrinksWindowThatNoLongerFitsWorkArea(t *testing.T) {
	displays := []displayArea{{ID: "primary", X: 0, Y: 0, Width: 1280, Height: 720, Primary: true}}
	saved := guiconfig.WindowPlacement{HasBounds: true, Display: "primary", X: 300, Y: 200, Width: 1800, Height: 1000}

	plan, ok := resolveWindowRestore("windows", saved, windowBounds{X: 0, Y: 0, Width: 1800, Height: 1000}, displays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	if plan.Normal.X != 0 || plan.Normal.Y != 0 || plan.Normal.Width != 1280 || plan.Normal.Height != 720 {
		t.Fatalf("restored bounds = %#v, want current work area", plan.Normal)
	}
}

func TestResolveWindowRestoreAccountsForWindowsDisplayDPI(t *testing.T) {
	displays := []displayArea{{ID: "scaled", X: 0, Y: 0, Width: 1920, Height: 1080, DPI: 144, Primary: true}}
	saved := guiconfig.WindowPlacement{HasBounds: true, Display: "scaled", X: 300, Y: 120, Width: 1280, Height: 800}

	plan, ok := resolveWindowRestore("windows", saved, windowBounds{X: 0, Y: 0, Width: 1280, Height: 800}, displays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	// Wails 的宽高是 96-DPI 逻辑单位；目标屏为 150%，1280 逻辑宽正好占满 1920 物理像素。
	if plan.Normal.X != 0 || plan.Normal.Y != 0 || plan.Normal.Width != 1280 || plan.Normal.Height != 720 {
		t.Fatalf("restored bounds = %#v, want DPI-aware work-area bounds", plan.Normal)
	}
}

func TestNormalizeCurrentWindowPlacementStoresWindowsCoordinatesRelativeToDisplay(t *testing.T) {
	got, ok := normalizeCurrentWindowPlacement("windows", windowBounds{X: -1200, Y: 80, Width: 1100, Height: 700}, dualDisplays)
	if !ok {
		t.Fatal("normalizeCurrentWindowPlacement returned ok=false")
	}
	want := (guiconfig.WindowPlacement{HasBounds: true, Display: "left", X: 720, Y: 80, Width: 1100, Height: 700})
	if got != want {
		t.Fatalf("placement = %#v, want %#v", got, want)
	}
}

func TestResolveWindowRestoreUsesCurrentMacScreenAsWailsCoordinateOrigin(t *testing.T) {
	displays := []displayArea{
		{ID: "mac:1", X: 0, Y: -1040, Width: 1920, Height: 1040, Primary: true, Current: true},
		{ID: "mac:2", X: 1920, Y: -1080, Width: 1920, Height: 1080},
	}
	saved := guiconfig.WindowPlacement{HasBounds: true, Display: "mac:2", X: 100, Y: 80, Width: 1100, Height: 700}
	current := windowBounds{X: 240, Y: 120, Width: 1100, Height: 700}

	plan, ok := resolveWindowRestore("darwin", saved, current, displays)
	if !ok {
		t.Fatal("resolveWindowRestore returned ok=false")
	}
	// macOS Wails 的 SetPosition 以当前屏幕可见工作区左上角为原点。
	// 右屏比主屏向右 1920 像素、顶部再高 40 像素。
	if plan.SetX != 2020 || plan.SetY != 40 {
		t.Fatalf("SetPosition = (%d, %d), want (2020, 40)", plan.SetX, plan.SetY)
	}
	if plan.Normal.Display != "mac:2" || plan.Normal.X != 100 || plan.Normal.Y != 80 {
		t.Fatalf("normalized placement = %#v", plan.Normal)
	}
}
