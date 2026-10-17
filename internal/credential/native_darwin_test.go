//go:build darwin && cgo

package credential

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// 显式启用后只操作唯一命名的合成条目，不读取已有用户凭据。
func TestNativeKeychainLegacyCompatibility(t *testing.T) {
	if os.Getenv("JUMPACCESS_TEST_KEYCHAIN") != "1" {
		t.Skip("set JUMPACCESS_TEST_KEYCHAIN=1 to test the macOS login keychain")
	}
	key := fmt.Sprintf("test/%d/%d/兼容", os.Getpid(), time.Now().UnixNano())
	backend := NewNativeBackend()
	if _, err := backend.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Get: %v", err)
	}
	if err := backend.Delete(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Delete: %v", err)
	}
	// security add-generic-password 使用传统文件型 Keychain，模拟旧版本写入。
	if output, err := exec.Command("/usr/bin/security", "add-generic-password", "-s", nativeTarget(key), "-a", "", "-w", "synthetic-legacy-value").CombinedOutput(); err != nil {
		t.Fatalf("seed legacy item: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if err := backend.Delete(key); err != nil && !errors.Is(err, ErrNotFound) {
			t.Errorf("cleanup: %v", err)
		}
	})
	got, err := backend.Get(key)
	if err != nil || string(got) != "synthetic-legacy-value" {
		t.Fatalf("legacy Get mismatch: %v", err)
	}
	for _, value := range [][]byte{{0, 1, 255, 0, 128}, {}, []byte("updated")} {
		if err := backend.Set(key, value); err != nil {
			t.Fatal(err)
		}
		got, err := backend.Get(key)
		if err != nil || !bytes.Equal(got, value) {
			t.Fatalf("updated Get mismatch: %v", err)
		}
	}
	if err := backend.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted Get: %v", err)
	}
	if err := backend.Set(key, []byte("new-item")); err != nil {
		t.Fatal(err)
	}
	if got, err := backend.Get(key); err != nil || string(got) != "new-item" {
		t.Fatalf("new Get mismatch: %v", err)
	}
}
