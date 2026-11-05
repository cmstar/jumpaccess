package sshhostkey

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
	"golang.org/x/crypto/ssh"
)

// 放大首次读取后、写入前的竞争窗口；测试不访问系统凭据存储。
type slowMissingBackend struct{ credential.Backend }

func (b slowMissingBackend) Get(key string) ([]byte, error) {
	value, err := b.Backend.Get(key)
	if errors.Is(err, credential.ErrNotFound) {
		time.Sleep(200 * time.Millisecond)
	}
	return value, err
}

func TestSignerStoreProcessHelper(t *testing.T) {
	root := os.Getenv("JUMPACCESS_SIGNER_TEST_ROOT")
	if root == "" {
		return
	}
	if _, err := io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	store := SignerStore{
		Backend: slowMissingBackend{credential.NewFileBackend(filepath.Join(root, "credentials"))},
		LockDir: filepath.Join(root, "locks"),
	}
	signer, err := store.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(ssh.FingerprintSHA256(signer.PublicKey()))
	os.Exit(0)
}

func TestSignerStoreConcurrentProcessesReusePersistedKey(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		start  io.WriteCloser
		output bytes.Buffer
	}
	children := make([]child, 4)
	for i := range children {
		c := &children[i]
		c.cmd = exec.CommandContext(ctx, executable, "-test.run=^TestSignerStoreProcessHelper$")
		c.cmd.Env = append(os.Environ(), "JUMPACCESS_SIGNER_TEST_ROOT="+root)
		c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
		c.start, err = c.cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = c.start.Close()
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		})
	}
	for i := range children {
		if _, err := children[i].start.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		_ = children[i].start.Close()
	}
	store := SignerStore{
		Backend: credential.NewFileBackend(filepath.Join(root, "credentials")),
		LockDir: filepath.Join(root, "locks"),
	}
	var fingerprints []string
	for i := range children {
		if err := children[i].cmd.Wait(); err != nil {
			t.Fatalf("child %d: %v\n%s", i, err, children[i].output.String())
		}
		fingerprints = append(fingerprints, strings.TrimSpace(children[i].output.String()))
	}
	persisted, err := store.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := ssh.FingerprintSHA256(persisted.PublicKey())
	for i, fingerprint := range fingerprints {
		if fingerprint != want {
			t.Errorf("process %d used %q; persisted key is %q", i, fingerprint, want)
		}
	}
}
