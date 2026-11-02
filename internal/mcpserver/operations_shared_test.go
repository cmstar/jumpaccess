package mcpserver_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/ssh"
)

// 仅在回环地址完成真实 SSH 握手与执行协议；命令文本由 fixture 解释，不运行本机 Shell。
func sharedOperationSSH(t *testing.T, handle func(ssh.Channel, <-chan *ssh.Request)) func(context.Context, agentmcp.ResolvedTarget) (*ssh.Client, error) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	stopped := false
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		stopped = true
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if stopped {
				mu.Unlock()
				_ = raw.Close()
				return
			}
			connections = append(connections, raw)
			mu.Unlock()
			go func() {
				defer raw.Close()
				_, channels, requests, err := ssh.NewServerConn(raw, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for request := range channels {
					ch, requests, err := request.Accept()
					if err == nil {
						go func() { defer ch.Close(); handle(ch, requests) }()
					}
				}
			}()
		}
	}()
	return func(ctx context.Context, _ agentmcp.ResolvedTarget) (*ssh.Client, error) {
		raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
		defer stop()
		c, channels, requests, err := ssh.NewClientConn(raw, listener.Addr().String(), &ssh.ClientConfig{User: "synthetic", HostKeyCallback: ssh.FixedHostKey(signer.PublicKey())})
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		return ssh.NewClient(c, channels, requests), nil
	}
}

func sharedOperationClient(t *testing.T, endpoint, token, name string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: name, Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: &http.Client{Transport: bearerTransport{token}, Timeout: 10 * time.Second}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func awaitSharedOperation(t *testing.T, session *mcp.ClientSession, id string) agentmcp.ExecutionView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var cursor uint64
	for time.Now().Before(deadline) {
		result := callOperation[agentmcp.ExecutionView](t, session, "get_execution", agentmcp.GetExecutionInput{ExecutionID: id, Cursor: cursor, WaitMS: 100})
		if !result.OK || result.Data == nil {
			t.Fatalf("查询执行失败：%+v", result)
		}
		cursor = result.Data.Output.NextCursor
		if result.Data.FinishedAt != nil {
			return *result.Data
		}
	}
	t.Fatal("执行未及时结束")
	return agentmcp.ExecutionView{}
}

func TestSharedOperationsAcrossHTTPClientsAndRuntimeRestart(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	longStarted, cancelStarted := make(chan struct{}), make(chan struct{})
	markLongStarted := sync.OnceFunc(func() { close(longStarted) })
	markCancelStarted := sync.OnceFunc(func() { close(cancelStarted) })
	var sends, signals, dials atomic.Int32
	dial := sharedOperationSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) {
		for request := range requests {
			if request.Type != "exec" {
				_ = request.Reply(false, nil)
				continue
			}
			var payload struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
				t.Error(err)
				return
			}
			sends.Add(1)
			_ = request.Reply(true, nil)
			switch {
			case strings.Contains(payload.Command, "long-shared"):
				markLongStarted()
				_, _ = fmt.Fprint(ch, "first\n")
				_, _ = fmt.Fprint(ch.Stderr(), "warning\n")
				<-gate
				_, _ = fmt.Fprint(ch, "done\n")
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{7}))
				return
			case strings.Contains(payload.Command, "cancel-shared"):
				markCancelStarted()
				for signal := range requests {
					if signal.Type == "signal" {
						signals.Add(1)
						_, _ = ch.SendRequest("exit-signal", false, ssh.Marshal(struct {
							Signal            string
							Core              bool
							Message, Language string
						}{Signal: "TERM"}))
						return
					}
					_ = signal.Reply(false, nil)
				}
				return
			default:
				t.Errorf("未预期的测试命令：%s", payload.Command)
				return
			}
		}
	})
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		engine := agentmcp.NewEngine(agentmcp.EngineOptions{
			Instance: info.ID,
			Resolve: func(_ context.Context, target agentmcp.Target) (agentmcp.ResolvedTarget, error) {
				return agentmcp.ResolvedTarget{Target: target, Site: "https://example.invalid", CredentialIdentity: "synthetic-login"}, nil
			},
			Validate: func(context.Context, agentmcp.ResolvedTarget) error { return nil },
			Dial: func(ctx context.Context, target agentmcp.ResolvedTarget) (*ssh.Client, error) {
				dials.Add(1)
				return dial(ctx, target)
			},
		})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine})
	})
	t.Cleanup(func() { _ = r.Stop(context.Background()) })
	t.Cleanup(release)
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := sharedOperationClient(t, status.Instance.URL, token, "agent-a")
	b := sharedOperationClient(t, status.Instance.URL, token, "agent-b")
	info := callOperation[mcpserver.ServerInfo](t, a, "get_server_info", map[string]any{})
	if !info.OK || info.Data == nil {
		t.Fatalf("服务信息不可读：%+v", info)
	}
	key := func(n int) string {
		return fmt.Sprintf("%s:%d:00000000-0000-4000-8000-%012d", status.Instance.ID, info.Data.RequestEpoch, n)
	}
	target := agentmcp.Target{Profile: "synthetic", Organization: "org", AssetID: "asset", AccountID: "account"}
	openInput := agentmcp.OpenInput{Target: target, RequestKey: key(1)}
	var wg sync.WaitGroup
	opened := make(chan mcpserver.Result[agentmcp.OpenResult], 2)
	for _, session := range []*mcp.ClientSession{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			opened <- callOperation[agentmcp.OpenResult](t, session, "open_connection", openInput)
		}()
	}
	wg.Wait()
	close(opened)
	connectionID, openCount := "", 0
	for result := range opened {
		if !result.OK || result.Data == nil || result.Data.Error != nil || result.Data.Connection == nil || result.Data.Status != "completed" {
			t.Fatalf("打开失败：%+v", result)
		}
		if connectionID == "" {
			connectionID = result.Data.Connection.ID
		}
		if result.Data.Connection.ID != connectionID {
			t.Fatal("跨 Client 同键打开产生不同连接")
		}
		openCount++
	}
	if openCount != 2 || dials.Load() != 1 {
		t.Fatalf("打开请求数=%d，SSH 拨号数=%d", openCount, dials.Load())
	}
	connection := callOperation[agentmcp.ConnectionView](t, b, "get_connection", agentmcp.ConnectionInput{ConnectionID: connectionID})
	connections := callOperation[agentmcp.Page[agentmcp.ConnectionView]](t, b, "list_connections", agentmcp.PageInput{})
	if !connection.OK || connection.Data == nil || !connection.Data.TransportConnected || !connections.OK || connections.Data == nil || connections.Data.Total != 1 || len(connections.Data.Items) != 1 || connections.Data.Items[0].ID != connectionID {
		t.Fatalf("共享连接不可见：get=%+v list=%+v", connection, connections)
	}
	zero := 0
	input := agentmcp.ExecuteInput{ConnectionID: connectionID, RequestKey: key(2), Command: "long-shared", Cwd: "/tmp", TimeoutStrategy: "local", WaitMS: &zero}
	executed := make(chan mcpserver.Result[agentmcp.ExecutionView], 2)
	for _, session := range []*mcp.ClientSession{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			executed <- callOperation[agentmcp.ExecutionView](t, session, "execute_command", input)
		}()
	}
	wg.Wait()
	close(executed)
	executionID, executeCount := "", 0
	for result := range executed {
		if !result.OK || result.Data == nil {
			t.Fatalf("执行失败：%+v", result)
		}
		if executionID == "" {
			executionID = result.Data.ID
		}
		if result.Data.ID != executionID {
			t.Fatal("跨 Client 同键执行产生多个任务")
		}
		executeCount++
	}
	if executeCount != 2 {
		t.Fatalf("执行请求完成数=%d", executeCount)
	}
	select {
	case <-longStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("SSH 未收到长命令")
	}
	busyInput := input
	busyInput.RequestKey = key(3)
	busy := callOperation[agentmcp.ExecutionView](t, b, "execute_command", busyInput)
	if busy.OK || busy.Error == nil || busy.Error.Code != "CONNECTION_BUSY" {
		t.Fatalf("不同请求没有被共享连接互斥：%+v", busy)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	jobs := callOperation[agentmcp.Page[agentmcp.ExecutionView]](t, b, "list_executions", agentmcp.ExecutionsPageInput{ListExecutionsInput: agentmcp.ListExecutionsInput{ConnectionID: connectionID}})
	if !jobs.OK || jobs.Data == nil || jobs.Data.Total != 1 || len(jobs.Data.Items) != 1 || jobs.Data.Items[0].ID != executionID {
		t.Fatalf("另一个 Client 找不到共享任务：%+v", jobs)
	}
	release()
	completed := awaitSharedOperation(t, b, executionID)
	if completed.Status != "completed" || completed.ExitCode == nil || *completed.ExitCode != 7 || completed.RemoteMayContinue || sends.Load() != 1 {
		t.Fatalf("Client 断开改变长任务：%+v sends=%d", completed, sends.Load())
	}
	output := callOperation[agentmcp.ExecutionView](t, b, "get_execution", agentmcp.GetExecutionInput{ExecutionID: executionID})
	if !output.OK || output.Data == nil {
		t.Fatalf("长任务结果不可读：%+v", output)
	}
	streams := map[string]string{}
	for _, chunk := range output.Data.Output.Chunks {
		streams[chunk.Stream] += chunk.Data
	}
	if streams["stdout"] != "first\ndone\n" || streams["stderr"] != "warning\n" {
		t.Fatalf("输出流不完整或串流：%v", streams)
	}
	retry := callOperation[agentmcp.ExecutionView](t, b, "execute_command", input)
	if !retry.OK || retry.Data == nil || retry.Data.ID != executionID || sends.Load() != 1 {
		t.Fatalf("跨 Client 终态重试重放：%+v sends=%d", retry, sends.Load())
	}
	cancelInput := input
	cancelInput.RequestKey, cancelInput.Command = key(4), "cancel-shared"
	cancellable := callOperation[agentmcp.ExecutionView](t, b, "execute_command", cancelInput)
	if !cancellable.OK || cancellable.Data == nil {
		t.Fatalf("取消测试任务未启动：%+v", cancellable)
	}
	select {
	case <-cancelStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("SSH 未收到可取消命令")
	}
	cancelled := callOperation[agentmcp.ExecutionView](t, b, "cancel_execution", agentmcp.CancelInput{ExecutionID: cancellable.Data.ID})
	if !cancelled.OK || cancelled.Data == nil || !cancelled.Data.CancelRequested {
		t.Fatalf("取消工具未请求停止：%+v", cancelled)
	}
	stopped := awaitSharedOperation(t, b, cancellable.Data.ID)
	if stopped.RemoteMayContinue || stopped.ExitSignal != "TERM" || signals.Load() != 1 || sends.Load() != 2 || dials.Load() != 1 {
		t.Fatalf("取消信号或共享传输异常：%+v signals=%d sends=%d dials=%d", stopped, signals.Load(), sends.Load(), dials.Load())
	}
	closed := callOperation[agentmcp.ConnectionView](t, b, "close_connection", agentmcp.CloseInput{ConnectionID: connectionID})
	if !closed.OK || closed.Data == nil || closed.Data.Status != "closed" || closed.Data.TransportConnected {
		t.Fatalf("关闭工具未释放连接：%+v", closed)
	}
	openInput.RequestKey = key(5)
	reopened := callOperation[agentmcp.OpenResult](t, b, "open_connection", openInput)
	if !reopened.OK || reopened.Data == nil || reopened.Data.Connection == nil || reopened.Data.Status != "completed" || reopened.Data.Connection.ID == connectionID || !reopened.Data.Connection.TransportConnected || dials.Load() != 2 {
		t.Fatalf("关闭后未建立新的逻辑连接：%+v dials=%d", reopened, dials.Load())
	}
	liveConnectionID := reopened.Data.Connection.ID
	previousInstance := status.Instance.ID
	status, err = r.Restart(context.Background())
	if err != nil || status.Instance == nil || status.Instance.ID == previousInstance {
		t.Fatalf("服务未创建新实例：%+v %v", status, err)
	}
	// 原有 HTTP Client 可以继续访问服务，但业务 ID 必须受服务实例约束。
	for _, oldID := range []string{connectionID, liveConnectionID} {
		stale := callOperation[agentmcp.ConnectionView](t, b, "get_connection", agentmcp.ConnectionInput{ConnectionID: oldID})
		if stale.OK || stale.Error == nil || stale.Error.Code != "INSTANCE_CHANGED" || stale.ServerInstanceID != status.Instance.ID {
			t.Fatalf("重启后旧连接 ID 未失效：%+v", stale)
		}
	}
	info = callOperation[mcpserver.ServerInfo](t, b, "get_server_info", map[string]any{})
	if !info.OK || info.Data == nil || info.Data.Counts == nil || info.Data.Counts.Connections != 0 || info.Data.Counts.SSHConnections != 0 || info.Data.Counts.Executions != 0 {
		t.Fatalf("重启继承了旧运行状态：%+v", info)
	}
}
