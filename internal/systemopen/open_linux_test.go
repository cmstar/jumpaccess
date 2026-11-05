package systemopen

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func installTestOpener(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestOpenLinuxReportsLauncherFailure(t *testing.T) {
	installTestOpener(t, "/bin/sleep 0.1\nexit 42\n")
	err := Open("https://example.invalid/authorize")
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 42 {
		t.Fatalf("Open error = %v, want launcher exit status 42", err)
	}
}

func TestOpenLinuxWaitsForSuccessfulLauncher(t *testing.T) {
	result := filepath.Join(t.TempDir(), "opened")
	t.Setenv("JUMPACCESS_OPEN_TEST_RESULT", result)
	installTestOpener(t, "/bin/sleep 0.1\nprintf '%s' \"$1\" > \"$JUMPACCESS_OPEN_TEST_RESULT\"\n")
	path := "/tmp/JumpAccess config.toml"
	if err := Open(path); err != nil {
		t.Fatal(err)
	}
	opened, err := os.ReadFile(result)
	if err != nil || string(opened) != path {
		t.Fatalf("opened = %q, error = %v", opened, err)
	}
}

func TestOpenLinuxReportsMissingLauncher(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Open("https://example.invalid/authorize"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("Open error = %v, want missing launcher", err)
	}
}
