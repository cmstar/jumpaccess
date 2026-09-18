package guiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusBarDefaultsForAllVersions(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			value, err := Decode([]byte(fmt.Sprintf("version = %d\n", version)))
			if err != nil {
				t.Fatal(err)
			}
			store := Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
			if err := store.Save(value); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(store.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "show_status_bar = true") {
				t.Fatal("旧配置应默认显示 SSH 状态栏")
			}
		})
	}
}

func TestStatusBarDisabledRoundTrip(t *testing.T) {
	value, err := Decode([]byte("[terminal]\nshow_status_bar = false\n"))
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
	if !strings.Contains(string(data), "show_status_bar = false") {
		t.Fatal("保存和重新读取后必须保留关闭值")
	}
}
