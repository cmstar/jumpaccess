package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func runtimeFixture(t *testing.T) (*agentmcp.Runtime, agentmcp.Settings, string) {
	t.Helper()
	root := t.TempDir()
	s := agentmcp.Settings{Store: config.Store{Path: filepath.Join(root, "config.toml")}, Credentials: credential.NewFileBackend(filepath.Join(root, "credentials")), Locker: filelock.Locker{Dir: filepath.Join(root, "locks")}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultMCP()
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err := s.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	token, err := s.GenerateToken(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	r := agentmcp.NewRuntime(s, "test", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token})
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := r.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return r, s, token
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(copy)
}

func TestHTTPMCPRealClientAndSingleInstanceLifecycle(t *testing.T) {
	r, settings, token := runtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started, err := r.Start(ctx)
	if err != nil || started.State != "running" || started.Instance == nil {
		t.Fatal("服务未就绪", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: started.Instance.URL, HTTPClient: &http.Client{Transport: bearerTransport{token}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != "2026-07-28" {
		t.Fatal("未协商新版 MCP 协议")
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "get_server_info" || listed.Tools[0].OutputSchema == nil {
		t.Fatal("工具发现或 schema 错误", err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_server_info", Arguments: map[string]any{}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatal("查询服务失败", err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	if strings.Contains(string(data), token) || !strings.Contains(string(data), started.Instance.ID) {
		t.Fatal("服务信息泄露 Token 或缺少实例 ID")
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_server_info", Arguments: map[string]any{"unexpected": true}})
	if err != nil || !invalid.IsError {
		t.Fatal("工具输入应拒绝未知字段", err)
	}
	second := agentmcp.NewRuntime(settings, "other", nil)
	if _, err := second.Start(ctx); !errors.Is(err, agentmcp.ErrRunning) {
		t.Fatal("未阻止第二个实例", err)
	}
	status, err := second.Status(ctx)
	if err != nil || status.State != "external" || status.Instance == nil || status.Instance.ID != started.Instance.ID {
		t.Fatal("无法识别外部实例", err)
	}
	cfg, _ := settings.Config()
	cfg.Port++
	if err := settings.SaveConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	status, err = r.Status(ctx)
	if err != nil || !status.RestartRequired || status.Instance.URL != started.Instance.URL {
		t.Fatal("待重启配置改变运行地址", err)
	}
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = second.Status(ctx)
	if err != nil || status.State != "stopped" || status.Instance != nil {
		t.Fatal("服务停止后仍宣称运行", err)
	}
	if _, err := settings.GenerateToken(ctx, true); err != nil {
		t.Fatal("停止后锁未释放", err)
	}
	if _, err := r.Start(ctx); err != nil {
		t.Fatal("服务无法重新启动", err)
	}
}

func TestHTTPMCPAuthenticationHostOriginAndLegacyProtocol(t *testing.T) {
	r, _, token := runtimeFixture(t)
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := status.Instance.URL
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`
	for _, tc := range []struct {
		name, token, host, origin string
		want                      int
	}{
		{"missing", "", "", "", 401}, {"wrong", "wrong", "", "", 401},
		{"host", token, "evil.example.test", "", 403}, {"origin", token, "", "https://evil.example.test", 403},
		{"null-origin", token, "", "null", 403}, {"legacy", token, "", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.want {
				t.Fatalf("HTTP %d，期望 %d：%s", resp.StatusCode, tc.want, data)
			}
			if strings.Contains(string(data), token) {
				t.Fatal("响应泄露 Token")
			}
			if tc.name == "legacy" && !bytes.Contains(data, []byte(`"protocolVersion":"2025-11-25"`)) {
				t.Fatal("没有协商旧版协议")
			}
		})
	}
}

func TestHTTPMCPStartFailureReleasesInstanceLock(t *testing.T) {
	r, settings, _ := runtimeFixture(t)
	cfg, _ := settings.Config()
	busy, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	if _, err := r.Start(context.Background()); err == nil {
		t.Fatal("端口被占用却报告成功")
	}
	if _, err := settings.GenerateToken(context.Background(), true); err != nil {
		t.Fatal("启动失败后实例锁未释放", err)
	}
}
