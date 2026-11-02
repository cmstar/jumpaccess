package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type guiMCPBearer struct{ token string }

func (b guiMCPBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestGUIMCPStartsExplicitlyAndStopsWithApplication(t *testing.T) {
	app, err := newDesktopApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.shutdown(context.Background()) })
	initial, err := app.GetMCPStatus()
	if err != nil || initial.State != "stopped" || initial.TokenConfigured {
		t.Fatal("GUI 初始化自动启动或生成了凭据", err)
	}
	if _, err := app.StartMCP(); err == nil {
		t.Fatal("没有 Token 时启动成功")
	}
	token, err := app.GenerateMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := initial.Config
	cfg.Token = token
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if _, err := app.SaveMCPConfig(cfg); err != nil {
		t.Fatal(err)
	}
	started, err := app.StartMCP()
	if err != nil || started.State != "running" {
		t.Fatal(err)
	}
	newToken, err := app.GenerateMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Token = newToken
	if saved, err := app.SaveMCPConfig(cfg); err != nil || !saved.RestartRequired {
		t.Fatal("运行中应可统一保存待应用的 Token", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "gui-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: started.Instance.URL, HTTPClient: &http.Client{Transport: guiMCPBearer{token}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	logs, err := app.ReadMCPLogs(0, 100)
	if err != nil || len(logs.Lines) == 0 || logs.Lines[0].Event != "service_started" {
		t.Fatal("缺少共享启动日志", err)
	}
	connections, err := app.ListMCPConnections()
	if err != nil || len(connections) != 0 {
		t.Fatal("空实例连接列表错误", err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_profiles"})
	if err != nil || result.IsError {
		t.Fatal("GUI 宿主无法提供资源工具", err)
	}
	queryLogs, err := app.ReadMCPLogs(logs.NextCursor, 100)
	if err != nil || len(queryLogs.Lines) != 2 || queryLogs.Lines[0].Detail != 2 || queryLogs.Lines[1].Detail != 3 || !strings.Contains(queryLogs.Lines[0].Text, "list_profiles") {
		t.Fatalf("GUI 未提供标准查询摘要和详细条件: %+v %v", queryLogs, err)
	}
	restarted, err := app.RestartMCP()
	if err != nil || restarted.RestartRequired || restarted.Instance.ID == started.Instance.ID {
		t.Fatal("GUI 重启未应用已保存的配置", err)
	}
	app.shutdown(context.Background())
	logs, err = app.ReadMCPLogs(logs.NextCursor, 100)
	if err != nil || len(logs.Lines) < 3 {
		t.Fatal("停止和重启未保留同一宿主的增量日志", err)
	}
	for _, line := range logs.Lines {
		if strings.Contains(line.Text, token) || strings.Contains(line.Text, newToken) {
			t.Fatal("日志泄漏 Token")
		}
	}
	stopped, err := app.GetMCPStatus()
	if err != nil || stopped.State != "stopped" {
		t.Fatal("GUI 退出未停止 MCP", err)
	}
	if unlock, err := app.core.MCPSettings.AcquireInstance(context.Background()); err != nil {
		t.Fatal("退出后未释放实例锁", err)
	} else {
		unlock()
	}
}

func TestGUIMCPRejectsInvalidPortBeforeSaving(t *testing.T) {
	root := t.TempDir()
	app, err := newDesktopApp(root)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.GetMCPStatus()
	if err != nil {
		t.Fatal(err)
	}
	state.Config.Token, err = app.GenerateMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{-1, 0, 65536, 100001} {
		state.Config.Port = port
		if _, err := app.SaveMCPConfig(state.Config); err == nil {
			t.Fatal("GUI 后端接受了无效端口")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("无效保存创建了配置文件")
	}
}

func TestMCPTLSFilePickerOptions(t *testing.T) {
	directory := t.TempDir()
	for _, kind := range []string{"certificate", "key"} {
		options, err := mcpTLSFileOptions(kind, filepath.Join(directory, "server.pem"))
		if err != nil || options.Title == "" || options.DefaultDirectory != directory || len(options.Filters) == 0 {
			t.Fatal("文件选择器应保留当前目录和过滤器", err)
		}
	}
	if _, err := mcpTLSFileOptions("unknown", ""); err == nil {
		t.Fatal("文件类型必须受限")
	}
}
