package guiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCopyOnEnterDefaultsForAllConfigVersions(t *testing.T) {
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
			if !strings.Contains(string(data), "copy_on_enter = true") {
				t.Fatal("缺少回车复制偏好的配置应默认开启并持久化")
			}
		})
	}
}

func TestCopyOnEnterDisabledRoundTrip(t *testing.T) {
	value, err := Decode([]byte("[terminal]\ncopy_on_enter = false\n"))
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
	if !reflect.DeepEqual(got, value) {
		t.Fatal("保存读取后配置不一致")
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "copy_on_enter = false") {
		t.Fatal("必须保留显式关闭值")
	}
}
