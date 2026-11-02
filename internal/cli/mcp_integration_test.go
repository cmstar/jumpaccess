package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/bootstrap"
	"github.com/cmstar/jumpaccess/internal/cli"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpIntegrationOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *mcpIntegrationOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *mcpIntegrationOutput) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type mcpIntegrationBearer struct{ token string }

func (b mcpIntegrationBearer) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestCLIMCPHostProvidesSharedToolsAndPreservesRunningToken(t *testing.T) {
	core, err := bootstrap.New(bootstrap.Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	var output mcpIntegrationOutput
	runtime := core.NewMCP("cli")
	deps := cli.Dependencies{Store: core.Store, MCPSettings: core.MCPSettings, MCP: runtime, Stdout: &output, Stderr: &output}
	configure := cli.NewRoot(deps)
	configure.SetArgs([]string{"mcp", "config", "--rotate-token", "--port", fmt.Sprint(port)})
	if err := configure.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := core.MCPSettings.Config()
	if err != nil {
		t.Fatal(err)
	}
	var logs mcpIntegrationOutput
	deps.Stdout, deps.Stderr = &logs, &logs
	serve := cli.NewRoot(deps)
	serve.SetArgs([]string{"mcp", "serve", "--color", "never", "--log-level", "detailed"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve.ExecuteContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = runtime.Stop(cleanup)
	})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.text(), "MCP 服务已启动") {
		select {
		case err := <-done:
			t.Fatalf("serve 提前返回: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("CLI 未启动服务")
		}
		time.Sleep(10 * time.Millisecond)
	}
	clientContext, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "cli-host-test", Version: "1"}, nil).Connect(clientContext, &mcp.StreamableClientTransport{
		Endpoint: cfg.URL(), HTTPClient: &http.Client{Transport: mcpIntegrationBearer{cfg.Token}}, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(clientContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_profiles", "open_connection", "execute_command", "cancel_execution", "download_files", "get_download", "cancel_download"} {
		if !names[want] {
			t.Fatalf("CLI 缺少共享工具 %s", want)
		}
	}
	for _, name := range []string{"list_profiles", "list_connections", "list_executions", "list_downloads"} {
		result, err := session.CallTool(clientContext, &mcp.CallToolParams{Name: name})
		if err != nil || result.IsError {
			t.Fatalf("CLI 工具 %s 调用失败: %v %+v", name, err, result)
		}
	}
	// 另一个 CLI 进程的配置修改必须保持当前实例的鉴权快照。
	management := deps
	management.MCP = core.NewMCP("cli")
	management.Stdout, management.Stderr = &output, &output
	rotate := cli.NewRoot(management)
	rotate.SetArgs([]string{"mcp", "config", "--rotate-token"})
	if err := rotate.Execute(); err != nil {
		t.Fatal(err)
	}
	updated, _ := core.MCPSettings.Config()
	if updated.Token == cfg.Token || !strings.Contains(output.text(), "重启") {
		t.Fatal("轮换未保存或缺少重启提示")
	}
	result, err := session.CallTool(clientContext, &mcp.CallToolParams{Name: "list_profiles"})
	if err != nil || result.IsError {
		t.Fatal("保存配置破坏了运行快照", err)
	}
	request, _ := http.NewRequestWithContext(clientContext, http.MethodGet, cfg.URL(), nil)
	request.Header.Set("Authorization", "Bearer "+updated.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("新Token不应立即生效: HTTP %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("取消未停止CLI宿主")
	}
	text := logs.text()
	if !strings.Contains(text, "list_profiles") || !strings.Contains(text, "MCP 服务已停止") || strings.Contains(text, "\x1b") || strings.Contains(text, cfg.Token) || strings.Contains(text, updated.Token) {
		t.Fatalf("CLI 日志缺失或混入配置/控制序列: %s", text)
	}
	connection, err := net.DialTimeout("tcp", cfg.Address(), 100*time.Millisecond)
	if err == nil {
		connection.Close()
		t.Fatal("退出后监听未释放")
	}
}
