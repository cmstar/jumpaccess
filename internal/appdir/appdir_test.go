package appdir

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestRootForWindowsUsesLocalAppData(t *testing.T) {
	windowsRoot := filepath.Join(`C:\Users\alice`, "AppData", "Local")

	got, err := RootFor("windows", windowsRoot, "")
	if err != nil {
		t.Fatalf("RootFor returned error: %v", err)
	}

	want := filepath.Join(windowsRoot, "JumpAccess")
	if got != want {
		t.Fatalf("RootFor() = %q, want %q", got, want)
	}
}

func TestRootForDarwinUsesApplicationSupport(t *testing.T) {
	got, err := RootFor("darwin", "", "/Users/alice/Library/Application Support")
	if err != nil {
		t.Fatalf("RootFor returned error: %v", err)
	}

	const want = "/Users/alice/Library/Application Support/JumpAccess"
	if got != want {
		t.Fatalf("RootFor() = %q, want %q", got, want)
	}
}

func TestRootForLinuxUsesUserConfigDirectory(t *testing.T) {
	got, err := RootFor("linux", "", "/home/alice/.config")
	if err != nil {
		t.Fatalf("RootFor returned error: %v", err)
	}

	const want = "/home/alice/.config/JumpAccess"
	if got != want {
		t.Fatalf("RootFor() = %q, want %q", got, want)
	}
}

func TestRootForWindowsRejectsMissingLocalAppData(t *testing.T) {
	if _, err := RootFor("windows", "", ""); err == nil {
		t.Fatal("RootFor() error = nil, want missing LOCALAPPDATA error")
	}
}

func TestRootForLinuxRejectsMissingUserConfigDirectory(t *testing.T) {
	if _, err := RootFor("linux", "", ""); err == nil {
		t.Fatal("RootFor() error = nil, want missing user config directory error")
	}
}

func TestRootUsesLinuxXDGOrHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux 用户配置目录规则")
	}
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "home", true: "xdg"}[custom], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			want := filepath.Join(home, ".config", "JumpAccess")
			if custom {
				config := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", config)
				want = filepath.Join(config, "JumpAccess")
			}
			got, err := Root()
			if err != nil || got != want {
				t.Fatalf("Root() = %q, %v; want %q", got, err, want)
			}
		})
	}
}
