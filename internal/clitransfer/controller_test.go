package clitransfer

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type capture struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data.Write(p)
}
func (c *capture) String() string { c.mu.Lock(); defer c.mu.Unlock(); return c.data.String() }
func waitText(t *testing.T, c *capture, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.String(), text) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("missing %q in %q", text, c.String())
}

func TestCLITransferWithIndependentPeer(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js required for independent peer")
	}
	if _, err := os.Stat(filepath.Join("..", "..", "cmd", "jumpaccess", "frontend", "node_modules", "zmodem.js")); err != nil {
		t.Skip("npm ci required for independent peer")
	}
	for _, mode := range []string{"send", "send-custom", "send-fallback", "send-disconnect", "receive"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			dir := t.TempDir()
			data := make([]byte, 70000)
			for i := range data {
				data[i] = byte(i)
			}
			source := filepath.Join(dir, "local file.bin")
			if err := os.WriteFile(source, data, 0600); err != nil {
				t.Fatal(err)
			}
			// 同名下载必须另存。
			if err := os.WriteFile(filepath.Join(dir, "binary.dat"), []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			peerMode := "send"
			if mode == "receive" {
				peerMode = "receive"
			}
			if mode == "send-disconnect" {
				peerMode = "send-pause"
			}
			cmd := exec.CommandContext(ctx, "node", "../zmodem/testdata/peer.cjs", peerMode, "70000")
			incoming, _ := cmd.StdoutPipe()
			remote, _ := cmd.StdinPipe()
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			keyboard, keys := io.Pipe()
			defer keyboard.Close()
			defer keys.Close()
			var display capture
			done := make(chan error, 1)
			options := Options{DefaultDirectory: func() (string, error) { return dir, nil }}
			if mode == "send-custom" {
				options.DownloadDirectory = dir
				options.DefaultDirectory = func() (string, error) { t.Error("explicit directory should override system default"); return "", nil }
			}
			if mode == "send-fallback" {
				options.DownloadDirectory = filepath.Join(dir, "missing")
			}
			go func() { done <- Run(ctx, keyboard, &display, remote, incoming, options) }()
			if mode == "receive" {
				waitText(t, &display, "Local file to upload")
				_, _ = io.WriteString(keys, "\""+source+"\"\r")
			}
			if mode == "send-fallback" {
				waitText(t, &display, "Local download directory")
				_, _ = io.WriteString(keys, dir+"\r")
			}
			if mode == "send-disconnect" {
				waitText(t, &display, "Download binary (1).dat")
				_ = cmd.Process.Kill()
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			processErr := cmd.Wait()
			if mode == "send-disconnect" {
				if _, err := os.Stat(filepath.Join(dir, "binary (1).dat")); !os.IsNotExist(err) {
					t.Fatal("disconnect left partial download")
				}
				return
			}
			if processErr != nil {
				t.Fatalf("peer: %v\n%s\n%s", processErr, diagnostic.String(), display.String())
			}
			if !strings.Contains(display.String(), "100%") || !strings.HasSuffix(display.String(), "shell$ ") {
				t.Fatalf("terminal = %q", display.String())
			}
			if mode != "receive" {
				got, err := os.ReadFile(filepath.Join(dir, "binary (1).dat"))
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("download: %v", err)
				}
			}
			original, _ := os.ReadFile(filepath.Join(dir, "binary.dat"))
			if string(original) != "existing" {
				t.Fatal("overwrote existing file")
			}
		})
	}
}

func TestOrdinaryOutputAndExpiredPromptNeverSendProtocol(t *testing.T) {
	for _, text := range []string{"你好\r\nshell$ ", "**\x18B0000000000ffff\r\n", "**\x18B0100000023be50\r\n\x11shell$ "} {
		var display, remote capture
		keyboard, keys := io.Pipe()
		defer keyboard.Close()
		defer keys.Close()
		err := Run(context.Background(), keyboard, &display, &remote, strings.NewReader(text), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if remote.String() != "" {
			t.Fatalf("sent bytes to an expired shell: %q", remote.String())
		}
		if strings.Contains(text, "shell$") && !strings.HasSuffix(display.String(), "shell$ ") {
			t.Fatalf("lost prompt: %q", display.String())
		}
	}
}

func TestCancelRestoresKeyboardWithoutSendingLocalInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	keyboard, keys := io.Pipe()
	defer keyboard.Close()
	defer keys.Close()
	incoming, server := io.Pipe()
	defer incoming.Close()
	defer server.Close()
	var display, remote capture
	done := make(chan error, 1)
	go func() { done <- Run(ctx, keyboard, &display, &remote, incoming, Options{}) }()
	_, _ = io.WriteString(server, "**\x18B0100000023be50\r\n\x11")
	waitText(t, &display, "Local file to upload")
	_, _ = io.WriteString(keys, "private-local-path\x03")
	waitText(t, &display, "Transfer cancelled")
	_, _ = io.WriteString(server, "shell$ ")
	waitText(t, &display, "shell$ ")
	_, _ = io.WriteString(keys, "pwd\r")
	waitText(t, &remote, "pwd\r")
	_ = server.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(remote.String(), "private-local-path") {
		t.Fatal("local path leaked into remote shell")
	}
}
