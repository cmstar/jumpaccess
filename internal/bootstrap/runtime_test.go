package bootstrap

import (
	"path/filepath"
	"testing"
	"time"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

func TestNewBuildsSharedRuntimeFromApplicationDirectory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	configuration := projectconfig.Default()
	configuration.Behavior.ConnectTimeout.Duration = 17 * time.Second
	if err := (projectconfig.Store{Path: configPath}).Save(configuration); err != nil {
		t.Fatal(err)
	}

	runtime, err := New(Options{RootDir: root})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if runtime.RootDir != root {
		t.Fatalf("RootDir = %q, want %q", runtime.RootDir, root)
	}
	if runtime.ConfigPath != configPath {
		t.Fatalf("ConfigPath = %q, want %q", runtime.ConfigPath, configPath)
	}
	if runtime.HTTPClient.Timeout != 17*time.Second {
		t.Fatalf("HTTP client timeout = %s, want 17s", runtime.HTTPClient.Timeout)
	}
	if runtime.Settings.Store.Path != configPath {
		t.Fatalf("settings store path = %q, want %q", runtime.Settings.Store.Path, configPath)
	}
}
