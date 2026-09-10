package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTerminalBackground(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "图片.png")
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aOZkAAAAASUVORK5CYII=")
	if err := os.WriteFile(path, png, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readTerminalBackground(path)
	if err != nil || !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("read image: %q %v", got, err)
	}
	for _, invalid := range []string{dir, filepath.Join(dir, "missing.png")} {
		if _, err := readTerminalBackground(invalid); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	if err := os.WriteFile(path, []byte("private non-image content"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readTerminalBackground(path); err == nil || got != "" {
		t.Fatal("returned non-image content")
	}
	if err := os.WriteFile(path, make([]byte, 20*1024*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTerminalBackground(path); err == nil {
		t.Fatal("accepted oversized file")
	}
}
