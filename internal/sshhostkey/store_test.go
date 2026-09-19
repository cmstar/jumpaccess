package sshhostkey

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"github.com/cmstar/jumpaccess/internal/filelock"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	callback, err := store.Callback(context.Background(), true)
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

	strict, err := store.Callback(context.Background(), false)
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
	callback, err := store.Callback(context.Background(), false)
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
	callback, _ := store.Callback(context.Background(), true)
	remote := &net.TCPAddr{}
	if err := callback("gateway.example.test:22", remote, testPublicKey(t)); err != nil {
		t.Fatal(err)
	}
	changed, _ := store.Callback(context.Background(), true)
	if err := changed("gateway.example.test:22", remote, testPublicKey(t)); err == nil {
		t.Fatal("changed key was accepted")
	}
}

func TestExistingCallbacksObserveNewlyTrustedKeys(t *testing.T) {
	store := Store{Path: t.TempDir() + "/known_hosts", Confirm: func(string, string) (bool, error) { return true, nil }}
	interactive, err := store.Callback(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	strict, err := store.Callback(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	key := testPublicKey(t)
	remote := &net.TCPAddr{}
	if err := interactive("gateway.example.test:22", remote, key); err != nil {
		t.Fatal(err)
	}
	if err := strict("gateway.example.test:22", remote, key); err != nil {
		t.Fatalf("已有 callback 未读取新增信任: %v", err)
	}
	if err := interactive("gateway.example.test:22", remote, testPublicKey(t)); err == nil {
		t.Fatal("已有 callback 接受了变更后的主机密钥")
	}
}

func TestTrustRechecksChangesMadeWhilePromptIsOpen(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(map[bool]string{true: "相同密钥不重复写入", false: "冲突密钥不能并存"}[sameKey], func(t *testing.T) {
			path := t.TempDir() + "/known_hosts"
			key := testPublicKey(t)
			otherKey := key
			if !sameKey {
				otherKey = testPublicKey(t)
			}
			remote := &net.TCPAddr{}
			other := Store{Path: path, Confirm: func(string, string) (bool, error) { return true, nil }}
			otherCallback, err := other.Callback(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			store := Store{Path: path, Confirm: func(host, _ string) (bool, error) {
				// 模拟另一 CLI/GUI 在当前用户确认前保存信任。
				return true, otherCallback(host, remote, otherKey)
			}}
			callback, err := store.Callback(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			err = callback("gateway.example.test:22", remote, key)
			if sameKey && err != nil {
				t.Fatal(err)
			}
			if !sameKey && err == nil {
				t.Fatal("确认期间变更的主机密钥仍被接受")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "\n") != 1 {
				t.Fatal("并发确认写入了重复或冲突的信任记录")
			}
		})
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

func TestCallbackCancelsWhileWaitingForTrustLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callback, err := (Store{Path: path}).Callback(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := (filelock.Locker{Dir: filepath.Join(filepath.Dir(path), "locks")}).Lock(context.Background(), "ssh-known-hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	done := make(chan error, 1)
	key := testPublicKey(t)
	go func() { done <- callback("gateway.example.test:22", &net.TCPAddr{}, key) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("锁等待未返回取消: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("主机信任锁等待忽略会话取消")
	}
}
