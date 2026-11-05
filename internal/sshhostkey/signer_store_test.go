package sshhostkey

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
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
	store := SignerStore{Backend: backend, LockDir: t.TempDir()}
	first, err := store.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.LoadOrCreate(context.Background())
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
	store := SignerStore{Backend: backend, LockDir: t.TempDir()}
	_, err := store.LoadOrCreate(context.Background())
	if err == nil || errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
	if string(backend.value) != "malformed" {
		t.Fatal("malformed key was replaced")
	}
	// 失败路径也必须释放锁，不能妨碍后续读取。
	backend.value = nil
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.LoadOrCreate(ctx); err != nil {
		t.Fatalf("lock was not released after parse failure: %v", err)
	}
}

func TestSignerStoreLockWaitHonorsContext(t *testing.T) {
	backend := &memorySecretBackend{}
	dir := t.TempDir()
	unlock, err := (filelock.Locker{Dir: dir}).Lock(context.Background(), "ssh-proxy-host-key")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = (SignerStore{Backend: backend, LockDir: dir}).LoadOrCreate(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline", err)
	}
	if backend.value != nil {
		t.Fatal("key was written without acquiring the lock")
	}
}

func TestSignerStoreRequiresLockDirectory(t *testing.T) {
	backend := &memorySecretBackend{}
	if _, err := (SignerStore{Backend: backend}).LoadOrCreate(context.Background()); err == nil {
		t.Fatal("missing lock directory was accepted")
	}
	if backend.value != nil {
		t.Fatal("key was written without a lock directory")
	}
}
