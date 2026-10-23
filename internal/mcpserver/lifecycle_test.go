package mcpserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConcurrentStartAndIndependentAgentSessions(t *testing.T) {
	r, _, token := runtimeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	startCtx, finishStart := context.WithCancel(ctx)
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := r.Start(startCtx)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- status.Instance.ID
		}()
	}
	wg.Wait()
	finishStart()
	close(ids)
	var id string
	for next := range ids {
		if id != "" && id != next {
			t.Fatal("并发启动产生两个实例")
		}
		id = next
	}
	status, err := r.Status(ctx)
	if err != nil || status.State != "running" {
		t.Fatal("启动请求结束后服务停止", err)
	}
	sessions := make([]*mcp.ClientSession, 2)
	for i := range sessions {
		session, err := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: status.Instance.URL, HTTPClient: &http.Client{Transport: bearerTransport{token}}, MaxRetries: -1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		sessions[i] = session
		defer session.Close()
	}
	if err := sessions[0].Close(); err != nil {
		t.Fatal(err)
	}
	result, err := sessions[1].CallTool(ctx, &mcp.CallToolParams{Name: "get_server_info"})
	if err != nil || result.IsError {
		t.Fatal("一个 Agent 断开影响另一个", err)
	}
}

func TestStopCancelsInflightQueryAndReleasesListener(t *testing.T) {
	_, settings, _ := runtimeFixture(t)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	r := agentmcp.NewRuntime(settings, "test", func(agentmcp.Instance, config.MCP, string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			close(entered)
			<-req.Context().Done()
			close(cancelled)
		})
	})
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = r.Stop(ctx)
	})
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, err := http.Get(status.Instance.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("请求未到达")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("退出未取消在途查询")
	}
	select {
	case <-clientDone:
	case <-ctx.Done():
		t.Fatal("查询未结束")
	}
	listener, err := net.Listen("tcp", status.Instance.Config.Address())
	if err != nil {
		t.Fatal("监听未释放", err)
	}
	listener.Close()
}

func TestTLSListenerUsesConfiguredCertificate(t *testing.T) {
	r, settings, token := runtimeFixture(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(filepath.Join(root, "cert.pem"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := settings.Config()
	cfg.AllowLAN, cfg.TLSCert, cfg.TLSKey = true, filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := settings.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(certPEM)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, status.Instance.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("TLS MCP 返回 %d", response.StatusCode)
	}
}

func TestLANHTTPStillRequiresTokenAndChecksOrigin(t *testing.T) {
	r, settings, token := runtimeFixture(t)
	cfg, err := settings.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ListenHost, cfg.AllowLAN = "0.0.0.0", true
	if err := settings.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal("LAN HTTP 配置应可保存", err)
	}
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(status.Instance.URL, "http://") {
		t.Fatal("未配置证书的 LAN 服务应使用 HTTP")
	}
	endpoint := strings.Replace(status.Instance.URL, "0.0.0.0", "127.0.0.1", 1)
	client := &http.Client{Timeout: 3 * time.Second}
	for _, tc := range []struct {
		name, token, origin, host string
		want                      int
	}{
		{name: "authorized", token: token, want: http.StatusOK},
		{name: "missing token", want: http.StatusUnauthorized},
		{name: "wrong token", token: "invalid-test-token", want: http.StatusUnauthorized},
		{name: "foreign origin", token: token, origin: "http://foreign.example.test", want: http.StatusForbidden},
		{name: "foreign host", token: token, host: "foreign.example.test:39287", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("MCP-Protocol-Version", "2025-11-25")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.host != "" {
				req.Host = tc.host
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.want {
				t.Fatalf("HTTP 状态 = %d, want %d", response.StatusCode, tc.want)
			}
		})
	}
}

type startupFailureHandler struct{ error }

func (h startupFailureHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (h startupFailureHandler) StartupError() error                          { return h.error }

func TestFactoryStartupFailureReleasesListenerAndInstanceLock(t *testing.T) {
	_, settings, _ := runtimeFixture(t)
	want := errors.New("领取缓存初始化失败")
	r := agentmcp.NewRuntime(settings, "test", func(agentmcp.Instance, config.MCP, string) http.Handler { return startupFailureHandler{want} })
	if _, err := r.Start(context.Background()); !errors.Is(err, want) {
		t.Fatalf("startup=%v", err)
	}
	status, err := r.Status(context.Background())
	if err != nil || status.State != "failed" || status.Instance != nil {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	unlock, err := settings.AcquireInstance(context.Background())
	if err != nil {
		t.Fatalf("startup failure retained instance lock: %v", err)
	}
	unlock()
	cfg, _ := settings.Config()
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		t.Fatalf("startup failure retained listener: %v", err)
	}
	listener.Close()
}

type shutdownWarningHandler struct {
	err   error
	stats agentmcp.EngineStats
}

func (h shutdownWarningHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (h shutdownWarningHandler) Shutdown(context.Context) error               { return h.err }
func (h shutdownWarningHandler) Stats() agentmcp.EngineStats                  { return h.stats }

func TestStopDistinguishesCleanupFailureFromUnknownRemoteWork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler shutdownWarningHandler
		remote  bool
	}{
		{name: "cache cleanup", handler: shutdownWarningHandler{err: errors.New("cache cleanup")}},
		{name: "unknown remote", handler: shutdownWarningHandler{stats: agentmcp.EngineStats{UnknownDownloads: 1}}, remote: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, settings, _ := runtimeFixture(t)
			r := agentmcp.NewRuntime(settings, "test", func(agentmcp.Instance, config.MCP, string) http.Handler { return tc.handler })
			if _, err := r.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := r.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			status, err := r.Status(context.Background())
			if err != nil || status.StopWarning == "" {
				t.Fatalf("missing cleanup warning: %+v %v", status, err)
			}
			if strings.Contains(status.StopWarning, "远端") != tc.remote {
				t.Fatalf("misleading cleanup warning: %s", status.StopWarning)
			}
		})
	}
}
