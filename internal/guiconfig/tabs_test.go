package guiconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTabPositionDefaultsForAllConfigVersions(t *testing.T) {
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
			if !strings.Contains(string(data), `new_tab_position = "end"`) {
				t.Fatal("默认新 Tab 位置应保存为 end")
			}
		})
	}
}

func TestNewTabPositionRoundTripAndValidation(t *testing.T) {
	for _, position := range []string{"end", "after_current"} {
		t.Run(position, func(t *testing.T) {
			value, err := Decode([]byte(fmt.Sprintf("[tabs]\nnew_tab_position = %q\nshow_close_buttons = false\n", position)))
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
			if got.Tabs != value.Tabs || got.Tabs.NewTabPosition != position || got.Tabs.ShowCloseButtons {
				t.Fatal("保存读取未保留 Tab 偏好")
			}
		})
	}
	for _, position := range []string{"", "right", "END"} {
		if _, err := Decode([]byte(fmt.Sprintf("[tabs]\nnew_tab_position = %q\n", position))); err == nil || !strings.Contains(err.Error(), "tabs.new_tab_position") {
			t.Fatalf("非法位置 %q 应返回校验错误，实际为 %v", position, err)
		}
	}
}
