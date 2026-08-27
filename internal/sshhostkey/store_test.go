package sshhostkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestInteractiveCallbackTrustsUnknownKeyAndPersistsIt(t *testing.T) {
	path := t.TempDir() + "/known_hosts"
	key := testPublicKey(t)
	prompts := 0
	store := Store{Path: path, Confirm: func(host, fingerprint string) (bool, error) {
		prompts++
		if host != "gateway.example.test:2222" || !strings.HasPrefix(fingerprint, "SHA256:") {
			t.Fatalf("prompt = %q %q", host, fingerprint)
		}
		return true, nil
	}}
	callback, err := store.Callback(true)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 2222}
	if err := callback("gateway.example.test:2222", remote, key); err != nil {
		t.Fatal(err)
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d", prompts)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "gateway.example.test") {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}

	strict, err := store.Callback(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := strict("gateway.example.test:2222", remote, key); err != nil {
		t.Fatalf("persisted key was rejected: %v", err)
	}
	if prompts != 1 {
		t.Fatalf("strict callback prompted; prompts = %d", prompts)
	}
}

func TestStrictCallbackRejectsUnknownKeyWithActionableMessage(t *testing.T) {
	store := Store{Path: t.TempDir() + "/known_hosts"}
	callback, err := store.Callback(false)
	if err != nil {
		t.Fatal(err)
	}
	err = callback("gateway.example.test:22", &net.TCPAddr{}, testPublicKey(t))
	if err == nil || !strings.Contains(err.Error(), "jumpctl ssh") {
		t.Fatalf("error = %v", err)
	}
}

func TestCallbackNeverAcceptsChangedHostKey(t *testing.T) {
	path := t.TempDir() + "/known_hosts"
	store := Store{Path: path, Confirm: func(string, string) (bool, error) { return true, nil }}
	callback, _ := store.Callback(true)
	remote := &net.TCPAddr{}
	if err := callback("gateway.example.test:22", remote, testPublicKey(t)); err != nil {
		t.Fatal(err)
	}
	changed, _ := store.Callback(true)
	if err := changed("gateway.example.test:22", remote, testPublicKey(t)); err == nil {
		t.Fatal("changed key was accepted")
	}
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
