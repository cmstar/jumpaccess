package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPDefaultsDoNotRewriteLegacyConfig(t *testing.T) {
	value, err := Decode([]byte("version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if value.MCP != nil || value.MCPSettings() != DefaultMCP() {
		t.Fatal("旧配置应使用缺省值且不创建 MCP 节")
	}
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[mcp]") {
		t.Fatal("保存旧配置不应自动添加新字段")
	}
}

func TestMCPConfigDecodeAndValidation(t *testing.T) {
	value, err := Decode([]byte("version = 1\n[mcp]\nport = 39288\n"))
	if err != nil {
		t.Fatal(err)
	}
	if value.MCPSettings().ListenHost != "127.0.0.1" || value.MCPSettings().Port != 39288 {
		t.Fatal("部分配置缺省值错误")
	}
	for _, raw := range []string{
		"port = 0", "port = 65536", "token = 'secret'", "listen_host = 'example.com'",
		"listen_host = '0.0.0.0'", "listen_host = '0.0.0.0'\nallow_lan = true",
		"tls_cert = 'cert.pem'", "tls_key = 'key.pem'",
	} {
		if _, err := Decode([]byte("version = 1\n[mcp]\n" + raw)); err == nil {
			t.Fatalf("应拒绝配置 %s", raw)
		}
	}
	for _, host := range []string{"127.0.0.1", "::1", "localhost"} {
		if _, err := Decode([]byte("version = 1\n[mcp]\nlisten_host = '" + host + "'")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Decode([]byte("version = 1\n[mcp]\nlisten_host = '0.0.0.0'\nallow_lan = true\ntls_cert = 'cert.pem'\ntls_key = 'key.pem'")); err != nil {
		t.Fatal(err)
	}
}
