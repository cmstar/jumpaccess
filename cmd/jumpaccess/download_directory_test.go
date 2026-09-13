package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

func TestChooseDownloadDirectory(t *testing.T) {
	for _, mode := range []string{"ask", "automatic", "remember", "custom"} {
		t.Run(mode, func(t *testing.T) {
			store := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
			value := guiconfig.Default()
			value.Downloads.Mode = mode
			value.Downloads.Directory = t.TempDir()
			value.Downloads.LastDirectory = t.TempDir()
			if err := store.Save(value); err != nil {
				t.Fatal(err)
			}
			downloads, selected := t.TempDir(), t.TempDir()
			calls := 0
			got, err := chooseDownloadDirectory(context.Background(), store, func() (string, error) { return downloads, nil }, func(start string) (string, error) {
				calls++
				want := downloads
				if mode == "remember" {
					want = value.Downloads.LastDirectory
				}
				if start != want {
					t.Fatalf("初始目录 = %q, want %q", start, want)
				}
				return selected, nil
			}, func(path string) (string, error) { return path, nil })
			want := selected
			if mode == "automatic" {
				want = downloads
			}
			if mode == "custom" {
				want = value.Downloads.Directory
			}
			if err != nil || got != want || (calls == 0) != (mode == "automatic" || mode == "custom") {
				t.Fatalf("选择结果 %q, %v, dialogs=%d", got, err, calls)
			}
			stored, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			wantLast := selected
			if mode == "automatic" || mode == "custom" {
				wantLast = value.Downloads.LastDirectory
			}
			if stored.Downloads.LastDirectory != wantLast {
				t.Fatalf("上次目录 = %q", stored.Downloads.LastDirectory)
			}
		})
	}
}

func TestChooseDownloadDirectoryFallbackAndCancellation(t *testing.T) {
	for _, scenario := range []string{"missing-last", "missing-default", "missing-custom", "cancel", "dialog-error", "disconnected"} {
		t.Run(scenario, func(t *testing.T) {
			store := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
			value := guiconfig.Default()
			value.Downloads.Mode = "remember"
			value.Downloads.LastDirectory = filepath.Join(t.TempDir(), "missing")
			if scenario == "missing-custom" {
				value.Downloads.Mode = "custom"
				value.Downloads.Directory = value.Downloads.LastDirectory
			}
			if scenario == "missing-default" {
				value.Downloads.Mode = "automatic"
			}
			if err := store.Save(value); err != nil {
				t.Fatal(err)
			}
			downloads, selected := t.TempDir(), t.TempDir()
			grantCalls := 0
			got, err := chooseDownloadDirectory(context.Background(), store, func() (string, error) {
				if scenario == "missing-default" {
					return "", errors.New("unavailable")
				}
				return downloads, nil
			}, func(start string) (string, error) {
				want := downloads
				if scenario == "missing-default" {
					want = ""
				}
				if start != want {
					t.Fatalf("回退目录 = %q, want %q", start, want)
				}
				if scenario == "cancel" {
					return "", nil
				}
				if scenario == "dialog-error" {
					return "", errors.New("dialog failed")
				}
				return selected, nil
			}, func(path string) (string, error) {
				grantCalls++
				if scenario == "disconnected" {
					return "", errors.New("closed")
				}
				return path, nil
			})
			failed := scenario == "dialog-error" || scenario == "disconnected"
			if (err != nil) != failed {
				t.Fatalf("错误 = %v", err)
			}
			stored, _ := store.Load()
			if failed || scenario == "cancel" {
				if got != "" || stored.Downloads.LastDirectory != value.Downloads.LastDirectory {
					t.Fatal("取消或失败不得更新上次目录")
				}
			} else if got != selected || stored.Downloads.LastDirectory != selected {
				t.Fatal("应授权并保存选择的目录")
			}
			if (scenario == "cancel" || scenario == "dialog-error") && grantCalls != 0 {
				t.Fatal("取消后不得授权目录")
			}
		})
	}
}
