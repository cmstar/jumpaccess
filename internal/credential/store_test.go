package credential

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type memoryBackend struct {
	values map[string][]byte
}

func (m *memoryBackend) Get(key string) ([]byte, error) {
	value, ok := m.values[key]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(value), nil
}

func (m *memoryBackend) Set(key string, value []byte) error {
	m.values[key] = bytes.Clone(value)
	return nil
}

func (m *memoryBackend) Delete(key string) error {
	if _, ok := m.values[key]; !ok {
		return ErrNotFound
	}
	delete(m.values, key)
	return nil
}

func TestRepositoryRoundTripsTokenPerProfile(t *testing.T) {
	backend := &memoryBackend{values: make(map[string][]byte)}
	repository := Repository{Backend: backend}
	want := Token{
		AccessToken:  "access-secret",
		RefreshToken: "refresh-secret",
		TokenType:    "Bearer",
		ClientID:     "client-id",
		Site:         "https://jump.example.test",
		ExpiresAt:    time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC),
		RefreshedAt:  time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}

	if err := repository.Save("work", want); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Load("work")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load = %#v, want %#v", got, want)
	}
	if _, ok := backend.values["oauth/work"]; !ok {
		t.Fatalf("backend keys = %#v", backend.values)
	}
}

func TestRepositoryReportsMissingAndMalformedCredentials(t *testing.T) {
	backend := &memoryBackend{values: map[string][]byte{"oauth/broken": []byte("not json")}}
	repository := Repository{Backend: backend}

	if _, err := repository.Load("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
	if _, err := repository.Load("broken"); err == nil {
		t.Fatal("malformed credential unexpectedly loaded")
	}
}

func TestRepositoryRejectsInvalidProfileKey(t *testing.T) {
	repository := Repository{Backend: &memoryBackend{values: make(map[string][]byte)}}
	for _, profile := range []string{"", "   "} {
		if err := repository.Save(profile, Token{AccessToken: "secret"}); err == nil {
			t.Fatalf("Save(%q) unexpectedly succeeded", profile)
		}
	}
}

func TestNativeTargetKeepsAllCredentialsUnderJumpAccessService(t *testing.T) {
	if got, want := nativeTarget("ssh/proxy-host-key"), "JumpAccess:ssh/proxy-host-key"; got != want {
		t.Fatalf("nativeTarget = %q, want %q", got, want)
	}
}

func TestRepositoryHasNoLegacyBackend(t *testing.T) {
	if _, exists := reflect.TypeOf(Repository{}).FieldByName("LegacyBackend"); exists {
		t.Fatal("Repository still exposes the obsolete OAuth legacy backend")
	}
}
