//go:build darwin

package credential

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileBackendRejectsCredentialWithBroadDarwinPermissions(t *testing.T) {
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
