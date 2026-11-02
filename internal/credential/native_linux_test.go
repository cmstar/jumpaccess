//go:build linux

package credential

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeBackendForRootStoresLinuxCredentialPrivately(t *testing.T) {
	root := t.TempDir()
	backend, err := NewNativeBackendForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Set("ssh/proxy-host-key", []byte("private-key")); err != nil {
		t.Fatal(err)
	}
	got, err := backend.Get("ssh/proxy-host-key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "private-key" {
		t.Fatalf("stored credential = %q, want private-key", got)
	}

	directory := filepath.Join(root, "ssh")
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("native credential directory permissions = %04o, want 0700", got)
	}
	keyPath := filepath.Join(directory, "proxy_host_ed25519_key")
	info, err = os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("ProxyCommand host key permissions = %04o, want 0600", got)
	}
}

func TestNativeBackendForRootRejectsOtherLinuxCredentialKeys(t *testing.T) {
	backend, err := NewNativeBackendForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Set("oauth/work", []byte("secret")); err == nil {
		t.Fatal("Linux ProxyCommand backend unexpectedly accepted another credential key")
	}
}

func TestNativeBackendForRootRejectsEmptyLinuxRoot(t *testing.T) {
	if _, err := NewNativeBackendForRoot(""); err == nil {
		t.Fatal("NewNativeBackendForRoot error = nil, want empty root error")
	}
}
