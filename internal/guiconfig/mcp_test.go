package guiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPAutoStartDefaultsOffForAllVersions(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		value, err := Decode([]byte(fmt.Sprintf("version = %d\n", version)))
		if err != nil {
			t.Fatal(err)
		}
		store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
		if err := store.Save(value); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "auto_start = false") {
			t.Fatalf("v%d 缺少 MCP 偏好时应默认关闭并持久化", version)
		}
	}
}

func TestMCPAutoStartRoundTrip(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		value, err := Decode([]byte(fmt.Sprintf("[mcp]\nauto_start = %t\n[appearance]\ntheme = 'dark'\n", enabled)))
		if err != nil {
			t.Fatal(err)
		}
		store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
		if err := store.Save(value); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load()
		if err != nil || got.Appearance.Theme != "dark" {
			t.Fatal("保存影响了其他偏好", err)
		}
		raw, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), fmt.Sprintf("auto_start = %t", enabled)) {
			t.Fatal("未保留自动启动偏好")
		}
	}
}
