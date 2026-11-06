package guiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrollbackDefaultsAndRoundTrip(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		t.Run(fmt.Sprintf("missing-v%d", version), func(t *testing.T) {
			checkScrollbackRoundTrip(t, fmt.Sprintf("version = %d\n", version), 10000)
		})
	}
	for _, lines := range []int{0, 1, 10000, 25000, 50000} {
		t.Run(fmt.Sprint(lines), func(t *testing.T) {
			checkScrollbackRoundTrip(t, fmt.Sprintf("version = 15\n[terminal]\nscrollback_lines = %d\n", lines), lines)
		})
	}
}

func checkScrollbackRoundTrip(t *testing.T, input string, lines int) {
	t.Helper()
	value, err := Decode([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("scrollback_lines = %d", lines)) {
		t.Fatalf("重新加载和保存后应保留 %d 行", lines)
	}
}

func TestScrollbackRejectsInvalidConfig(t *testing.T) {
	for _, value := range []string{"-1", "50001", "1000000", "1.5", "\"10000\""} {
		t.Run(value, func(t *testing.T) {
			if _, err := Decode([]byte("[terminal]\nscrollback_lines = " + value)); err == nil {
				t.Fatal("必须拒绝非整数或超出 0 到 50000 的历史行数")
			}
		})
	}
	for _, lines := range []int{-1, 50001} {
		value := Default()
		value.Terminal.ScrollbackLines = lines
		store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
		if err := store.Save(value); err == nil {
			t.Fatal("保存入口也必须拒绝越界的历史行数")
		}
	}
}
