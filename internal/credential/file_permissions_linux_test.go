//go:build linux

package credential

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileBackendUsesPrivateLinuxPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	backend := NewFileBackend(directory)
	if err := backend.Set("oauth/work", []byte(`{"access_token":"secret"}`)); err != nil {
		t.Fatal(err)
	}

	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("credential directory permissions = %04o, want 0700", got)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("credential file count = %d, want 1", len(entries))
	}
	fileInfo, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("credential file permissions = %04o, want 0600", got)
	}
}

func TestFileBackendRejectsCredentialWithBroadLinuxPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	backend := NewFileBackend(directory)
	if err := backend.Set("oauth/work", []byte(`{"access_token":"secret"}`)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, entries[0].Name())
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := backend.Get("oauth/work"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want insecure permissions rejection", err)
	}
}

func TestFileBackendRejectsLinuxSymlink(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	backend := NewFileBackend(directory)
	if err := backend.Set("oauth/work", []byte(`{"access_token":"secret"}`)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, entries[0].Name())
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if _, err := backend.Get("oauth/work"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get error = %v, want symlink rejection", err)
	}
}

func TestFileBackendRejectsLinuxSymlinkDirectoryWithoutChangingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "credentials")
	if err := os.Symlink(target, directory); err != nil {
		t.Fatal(err)
	}

	backend := NewFileBackend(directory)
	if err := backend.Set("oauth/work", []byte(`{"access_token":"secret"}`)); err == nil {
		t.Fatal("Set unexpectedly accepted a symlink credential directory")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("symlink target permissions = %04o, want unchanged 0755", got)
	}
}
