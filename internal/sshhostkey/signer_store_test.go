package sshhostkey

import (
	"bytes"
	"errors"
	"testing"

	"github.com/cmstar/jumpaccess/internal/credential"
)

type memorySecretBackend struct{ value []byte }

func (m *memorySecretBackend) Get(string) ([]byte, error) {
	if m.value == nil {
		return nil, credential.ErrNotFound
	}
	return bytes.Clone(m.value), nil
}

func (m *memorySecretBackend) Set(_ string, value []byte) error {
	m.value = bytes.Clone(value)
	return nil
}

func (m *memorySecretBackend) Delete(string) error {
	if m.value == nil {
		return credential.ErrNotFound
	}
	m.value = nil
	return nil
}

func TestSignerStoreCreatesAndReusesStableEd25519HostKey(t *testing.T) {
	backend := &memorySecretBackend{}
	store := SignerStore{Backend: backend}
	first, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey().Type() != "ssh-ed25519" || !bytes.Equal(first.PublicKey().Marshal(), second.PublicKey().Marshal()) {
		t.Fatalf("signers are not the same stable Ed25519 key")
	}
	if len(backend.value) == 0 {
		t.Fatal("private key was not persisted")
	}
}

func TestSignerStoreDoesNotReplaceMalformedStoredKey(t *testing.T) {
	backend := &memorySecretBackend{value: []byte("malformed")}
	_, err := (SignerStore{Backend: backend}).LoadOrCreate()
	if err == nil || errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
	if string(backend.value) != "malformed" {
		t.Fatal("malformed key was replaced")
	}
}
