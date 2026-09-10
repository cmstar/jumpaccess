package guiconfig

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestBackgroundDefaultsAndRoundTrip(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		value, err := Decode([]byte(fmt.Sprintf("version = %d\n", version)))
		if err != nil {
			t.Fatal(err)
		}
		bg := value.Terminal.Background
		if bg.Enabled || bg.FilePath != "" || bg.TransparencyPercent != 70 || bg.FitMode != "cover" || bg.PositionXPercent != 50 || bg.PositionYPercent != 50 {
			t.Fatalf("version %d: %#v", version, bg)
		}
	}
	value := Default()
	value.Terminal.Background = Background{Enabled: true, FilePath: "D:/图片/wallpaper.png", TransparencyPercent: 0, FitMode: "tile", PositionXPercent: 0, PositionYPercent: 100, TileFitLongEdge: true, TileOnlyWholeTiles: true}
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Terminal.Background != value.Terminal.Background {
		t.Fatalf("lost background: %#v", got.Terminal.Background)
	}
}

func TestBackgroundValidation(t *testing.T) {
	for _, change := range []func(*Background){
		func(b *Background) { b.FitMode = "unknown" },
		func(b *Background) { b.TransparencyPercent = -1 },
		func(b *Background) { b.TransparencyPercent = 96 },
		func(b *Background) { b.PositionXPercent = 101 },
		func(b *Background) { b.PositionYPercent = -1 },
		func(b *Background) { b.FilePath = "bad\x00.png" },
	} {
		value := Default()
		change(&value.Terminal.Background)
		if err := value.Validate(); err == nil {
			t.Fatalf("accepted invalid background: %#v", value.Terminal.Background)
		}
	}
}
