package cli

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

func TestMCPConfigViewDoesNotCreateConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var output bytes.Buffer
	root := NewRoot(Dependencies{Store: config.Store{Path: path}, Stdout: &output})
	root.SetArgs([]string{"mcp", "config"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"listen-host", "allow-lan", "tls-cert", "tls-key", "127.0.0.1", "39287", "token", "未配置", "http://127.0.0.1:39287/mcp"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("配置缺少 %q: %s", want, &output)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("查看配置写入了文件: %v", err)
	}
}

func executeMCPConfig(t *testing.T, deps Dependencies, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	deps.Stdout = &output
	root := NewRoot(deps)
	root.SetArgs(append([]string{"mcp"}, args...))
	err := root.Execute()
	return output.String(), err
}

func TestMCPConfigUpdatesEchoFullConfigurationAndRotateAtomically(t *testing.T) {
	store := config.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	deps := Dependencies{Store: store}
	first, err := executeMCPConfig(t, deps, "config", "--rotate-token", "--port", "39288", "--listen-host", "0.0.0.0", "--allow-lan=true")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	initial := saved.MCPSettings()
	if err := config.ValidateMCPToken(initial.Token); err != nil {
		t.Fatal(err)
	}
	if initial.Port != 39288 || initial.ListenHost != "0.0.0.0" || !initial.AllowLAN || !strings.Contains(first, initial.Token) {
		t.Fatal("首次配置或回显不完整")
	}
	view, err := executeMCPConfig(t, deps, "config")
	if err != nil || first != view {
		t.Fatalf("保存回显与查看配置不一致: %v\n%s\n%s", err, first, view)
	}
	if _, err := executeMCPConfig(t, deps, "config", "--rotate-token", "--port", "39289"); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.Load()
	rotated := saved.MCPSettings()
	if rotated.Token == initial.Token || rotated.Port != 39289 || rotated.ListenHost != initial.ListenHost || rotated.AllowLAN != initial.AllowLAN {
		t.Fatal("轮换未保留未指定字段")
	}
	before, _ := os.ReadFile(store.Path)
	for _, flags := range [][]string{
		{"--port", "0", "--rotate-token"}, {"--allow-lan=false"}, {"--token", "invalid"},
		{"--token", initial.Token, "--rotate-token"}, {"--no-tls", "--tls-cert", "missing.crt"},
		{"--tls-cert", "missing.crt"},
	} {
		out, err := executeMCPConfig(t, deps, append([]string{"config"}, flags...)...)
		if err == nil || out != "" {
			t.Fatalf("无效修改成功或回显了未保存值: %v %q", err, out)
		}
		after, _ := os.ReadFile(store.Path)
		if !bytes.Equal(before, after) {
			t.Fatal("无效修改改变了原文件")
		}
	}
	view, err = executeMCPConfig(t, deps, "config", "--token", initial.Token, "--listen-host", "127.0.0.1", "--allow-lan=false")
	if err != nil || !strings.Contains(view, initial.Token) {
		t.Fatalf("手动 Token 或关闭 LAN 失败: %v", err)
	}
	saved, err = store.Load()
	if err != nil || saved.MCPSettings().AllowLAN || saved.MCPSettings().ListenHost != "127.0.0.1" {
		t.Fatal("显式 --allow-lan=false 未保存", err)
	}
}

func TestMCPConfigRequiresTokenAndRejectsRemovedTokenCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	deps := Dependencies{Store: config.Store{Path: path}}
	for _, args := range [][]string{{"config", "--port", "39288"}, {"token", "generate"}, {"config", "unexpected"}, {"config", "set", "--rotate-token"}, {"config", "get"}} {
		if out, err := executeMCPConfig(t, deps, args...); err == nil || out != "" {
			t.Fatalf("未拒绝 %v: %q %v", args, out, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("失败命令产生配置")
	}
}

func TestMCPConfigHelpDoesNotSave(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--help=false"}, {"--rotate-token", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			output, err := executeMCPConfig(t, Dependencies{Store: config.Store{Path: path}}, append([]string{"config"}, args...)...)
			if err != nil || output == "" {
				t.Fatalf("查看配置或帮助失败: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("帮助选项触发保存: %v", err)
			}
		})
	}
}

func TestMCPConfigSetsAndClearsTLSAsOneChange(t *testing.T) {
	dir := t.TempDir()
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := tlsServer.TLS.Certificates[0]
	tlsServer.Close()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "test.crt"), filepath.Join(dir, "test.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	store := config.Store{Path: filepath.Join(dir, "config.toml")}
	deps := Dependencies{Store: store}
	output, err := executeMCPConfig(t, deps, "config", "--rotate-token", "--tls-cert", certPath, "--tls-key", keyPath)
	if err != nil || !strings.Contains(output, "https://") {
		t.Fatal("成对保存TLS失败", err)
	}
	before, _ := store.Load()
	output, err = executeMCPConfig(t, deps, "config", "--no-tls")
	if err != nil || !strings.Contains(output, "http://") {
		t.Fatal("关闭TLS失败", err)
	}
	after, _ := store.Load()
	if after.MCPSettings().TLSCert != "" || after.MCPSettings().TLSKey != "" || after.MCPSettings().Token != before.MCPSettings().Token {
		t.Fatal("关闭TLS未同时清除两项或改变了Token")
	}
}

func TestMCPCommandPrefixesPreserveArgumentsAndRejectAmbiguity(t *testing.T) {
	store := config.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	root := NewRoot(Dependencies{Store: store})
	root.SetArgs([]string{"m", "c", "--rotate-token"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root = NewRoot(Dependencies{Store: store})
	root.SetArgs([]string{"mcp", "s"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "ambiguous command") || !strings.Contains(err.Error(), "serve") || !strings.Contains(err.Error(), "status") {
		t.Fatalf("serve/status 缩写未报歧义: %v", err)
	}
}

type brokenMCPConfigWriter struct{}

func (brokenMCPConfigWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMCPConfigReportsSavedWhenEchoFails(t *testing.T) {
	store := config.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	root := NewRoot(Dependencies{Store: store, Stdout: brokenMCPConfigWriter{}})
	root.SetArgs([]string{"mcp", "config", "--rotate-token"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "已保存") || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("未区分保存与输出失败: %v", err)
	}
	value, err := store.Load()
	if err != nil || value.MCPSettings().Token == "" {
		t.Fatal("配置未保存", err)
	}
}

func TestMCPStatusAndConfigObserveGUIHostWithoutReconfiguringIt(t *testing.T) {
	dir := t.TempDir()
	settings := agentmcp.Settings{Store: config.Store{Path: filepath.Join(dir, "config.toml")}, Locker: filelock.Locker{Dir: filepath.Join(dir, "locks")}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultMCP()
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg.Token, _ = settings.GenerateToken(context.Background())
	if err := settings.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	gui := agentmcp.NewRuntime(settings, "gui", func(agentmcp.Instance, config.MCP, string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	if _, err := gui.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = gui.Stop(ctx)
	})
	deps := Dependencies{MCPSettings: settings, MCP: agentmcp.NewRuntime(settings, "cli", nil)}
	status, err := executeMCPConfig(t, deps, "status")
	if err != nil || !strings.Contains(status, "gui") || !strings.Contains(status, cfg.URL()) || strings.Contains(status, cfg.Token) {
		t.Fatalf("实例状态不完整或输出Token: %v %s", err, status)
	}
	out, err := executeMCPConfig(t, deps, "config", "--rotate-token")
	if err != nil || !strings.Contains(out, "重启") {
		t.Fatal("保存后未提示重启", err)
	}
	current, err := gui.Status(context.Background())
	if err != nil || current.State != "running" || !current.RestartRequired || current.Instance.URL != cfg.URL() {
		t.Fatal("配置修改影响运行实例", err)
	}
}
