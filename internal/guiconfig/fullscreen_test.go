package guiconfig

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFullscreenToolbarPreferenceDefaultsAndRoundTrips(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		value, err := Decode([]byte(fmt.Sprintf("version = %d\n", version)))
		if err != nil {
			t.Fatal(err)
		}
		if !value.Terminal.FullscreenHideToolbar {
			t.Fatalf("version %d: 默认应隐藏全屏工具栏", version)
		}
	}
	value, err := Decode([]byte("[terminal]\nfullscreen_hide_toolbar = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Terminal.FullscreenHideToolbar {
		t.Fatal("显式关闭值必须在重启后保留")
	}
}
