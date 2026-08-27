package appdir

import (
	"path/filepath"
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

func TestRootForWindowsRejectsMissingLocalAppData(t *testing.T) {
	if _, err := RootFor("windows", "", ""); err == nil {
		t.Fatal("RootFor() error = nil, want missing LOCALAPPDATA error")
	}
}
