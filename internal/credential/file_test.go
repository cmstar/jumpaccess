package credential

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFileBackendStoresOneOpaqueFilePerProfile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	repository := Repository{Backend: NewFileBackend(directory)}
	tokens := map[string]Token{
		"work":          {AccessToken: "work-access"},
		"team/研发:CON?*": {AccessToken: "team-access"},
	}

	for profile, token := range tokens {
		if err := repository.Save(profile, token); err != nil {
			t.Fatalf("Save(%q): %v", profile, err)
		}
	}
	for profile, want := range tokens {
		got, err := repository.Load(profile)
		if err != nil {
			t.Fatalf("Load(%q): %v", profile, err)
		}
		if got.AccessToken != want.AccessToken {
			t.Fatalf("Load(%q).AccessToken = %q, want %q", profile, got.AccessToken, want.AccessToken)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(tokens) {
		t.Fatalf("credential file count = %d, want %d", len(entries), len(tokens))
	}
	validName := regexp.MustCompile(`^[0-9a-f]{64}\.json$`)
	wantNames := make(map[string]bool, len(tokens))
	for profile := range tokens {
		digest := sha256.Sum256([]byte("oauth/" + profile))
		wantNames[fmt.Sprintf("%x.json", digest)] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || !validName.MatchString(entry.Name()) {
			t.Fatalf("credential filename %q is not an opaque SHA-256 JSON name", entry.Name())
		}
		if !wantNames[entry.Name()] {
			t.Fatalf("credential filename %q was not derived from an exact profile name", entry.Name())
		}
		for profile := range tokens {
			if strings.Contains(entry.Name(), profile) {
				t.Fatalf("credential filename %q exposes profile %q", entry.Name(), profile)
			}
		}
	}
}

func TestFileBackendAtomicallyReplacesAndDeletesCredential(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "credentials")
	repository := Repository{Backend: NewFileBackend(directory)}

	if err := repository.Save("work", Token{AccessToken: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Save("work", Token{AccessToken: "second"}); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Load("work")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "second" {
		t.Fatalf("AccessToken = %q, want second", got.AccessToken)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("credential file count = %d, want 1", len(entries))
	}

	if err := repository.Delete("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Load("work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after Delete error = %v, want ErrNotFound", err)
	}
}

func TestFileBackendRejectsEmptyStorageKey(t *testing.T) {
	backend := NewFileBackend(filepath.Join(t.TempDir(), "credentials"))
	if err := backend.Set("", []byte("secret")); err == nil {
		t.Fatal("Set unexpectedly accepted an empty key")
	}
}

func TestFileBackendRejectsEmptyCredentialDirectory(t *testing.T) {
	if err := NewFileBackend("").Set("oauth/work", []byte("secret")); err == nil {
		t.Fatal("Set unexpectedly accepted an empty credential directory")
	}
}
