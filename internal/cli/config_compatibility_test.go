package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

func TestCLIValidatesAndUpdatesConfigurationWithMCPFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 1\n[mcp]\nenabled = true\nport = 19876\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "validate"}, {"profile", "add", "work", "--url", "https://jump.example.test"}} {
		var output bytes.Buffer
		root := NewRoot(Dependencies{Store: projectconfig.Store{Path: path}, ConfigPath: path, Stdout: &output})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	var got struct {
		MCP struct {
			Enabled bool
			Port    int
		}
	}
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatal(err)
	}
	if !got.MCP.Enabled || got.MCP.Port != 19876 {
		t.Fatal("CLI update lost MCP configuration")
	}
}
