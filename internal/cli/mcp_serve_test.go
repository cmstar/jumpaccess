package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	"github.com/cmstar/jumpaccess/internal/filelock"
)

type serveTestBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *serveTestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *serveTestBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func serveTestFixture(t *testing.T) (*agentmcp.Runtime, agentmcp.Settings, config.MCP) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	directory := t.TempDir()
	settings := agentmcp.Settings{Store: config.Store{Path: filepath.Join(directory, "config.toml")}, Locker: filelock.Locker{Dir: filepath.Join(directory, "locks")}}
	cfg := config.DefaultMCP()
	cfg.Port, cfg.Token = port, strings.Repeat("A", 43)
	if err := settings.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	runtime := agentmcp.NewRuntime(settings, "cli", func(agentmcp.Instance, config.MCP, string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ready") })
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Stop(ctx)
	})
	return runtime, settings, cfg
}

func startServeTest(t *testing.T, runtime *agentmcp.Runtime, stdout, stderr io.Writer, args ...string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	command := newMCPServeCommand(Dependencies{MCP: runtime})
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs(args)
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(ctx) }()
	t.Cleanup(cancel)
	return cancel, done
}

func awaitServeTest(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待 MCP serve 状态超时")
}

func stopServeTest(t *testing.T, cancel context.CancelFunc, done <-chan error) error {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		return err
	case <-time.After(7 * time.Second):
		t.Fatal("MCP serve 未在有限时间内退出")
		return nil
	}
}

func TestMCPServeBlocksStreamsLogsAndStopsWithoutClearing(t *testing.T) {
	runtime, _, cfg := serveTestFixture(t)
	var stdout, stderr serveTestBuffer
	cancel, done := startServeTest(t, runtime, &stdout, &stderr)
	awaitServeTest(t, func() bool { return strings.Contains(stdout.String(), "MCP 服务已启动") })
	response, err := (&http.Client{Timeout: time.Second}).Get(cfg.URL())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("前台命令未提供 HTTP 服务")
	}
	runtime.History().Record("", "tool_call", "INFO", 2, "normal-call")
	runtime.History().Record("", "tool_conditions", "DEBUG", 3, "hidden-details")
	runtime.History().Record("", "execution_finished", "INFO", 2, "command-result exit=7")
	runtime.History().Record("", "execution_finished", "WARN", 1, "unknown-result")
	runtime.History().Record("", "tool_call", "ERROR", 1, "failed-call")
	awaitServeTest(t, func() bool { return strings.Contains(stderr.String(), "failed-call") })
	select {
	case err := <-done:
		t.Fatalf("调用错误不应结束宿主: %v", err)
	default:
	}
	if err := stopServeTest(t, cancel, done); err != nil {
		t.Fatal(err)
	}
	out, diagnostic := stdout.String(), stderr.String()
	for _, expected := range []string{"normal-call", "command-result exit=7", "MCP 服务已停止"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("stdout 缺少 %q: %s", expected, out)
		}
	}
	if strings.Contains(out+diagnostic, "hidden-details") || strings.Contains(out, "failed-call") || strings.Contains(diagnostic, "normal-call") || !strings.Contains(diagnostic, "unknown-result") {
		t.Fatalf("日志详细程度或分流错误\nstdout: %s\nstderr: %s", out, diagnostic)
	}
	if strings.Contains(out+diagnostic, "\x1b") {
		t.Fatal("非终端自动模式不应包含 ANSI 或清屏控制序列")
	}
	if connection, err := net.DialTimeout("tcp", cfg.Address(), 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("退出后仍在监听")
	}
}

func TestMCPServeDetailedANSIAndExplicitNever(t *testing.T) {
	for _, mode := range []string{"always", "never"} {
		t.Run(mode, func(t *testing.T) {
			runtime, _, _ := serveTestFixture(t)
			var stdout, stderr serveTestBuffer
			t.Setenv("NO_COLOR", "1")
			t.Setenv("TERM", "dumb")
			cancel, done := startServeTest(t, runtime, &stdout, &stderr, "--log-level", "detailed", "--color", mode)
			awaitServeTest(t, func() bool { return strings.Contains(stdout.String(), "MCP 服务已启动") })
			runtime.History().Record("", "tool_conditions", "DEBUG", 3, "visible-details")
			runtime.History().Record("", "tool_call", "ERROR", 1, "colored-error")
			awaitServeTest(t, func() bool { return strings.Contains(stderr.String(), "colored-error") })
			if err := stopServeTest(t, cancel, done); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "visible-details") {
				t.Fatal("详细模式遗漏查询条件")
			}
			if mode == "always" {
				if !strings.Contains(stdout.String(), "\x1b[36m") || !strings.Contains(stderr.String(), "\x1b[31m") {
					t.Fatal("显式颜色未同时应用到两个输出流")
				}
			} else if strings.Contains(stdout.String()+stderr.String(), "\x1b") {
				t.Fatal("never 模式仍包含控制序列")
			}
			for _, forbidden := range []string{"\x1b[2J", "\x1b[3J", "\x1b[?1049", "\x1b[?47"} {
				if strings.Contains(stdout.String()+stderr.String(), forbidden) {
					t.Fatal("服务输出不得清屏或使用备用屏幕")
				}
			}
		})
	}
}

func TestMCPServeMinimalFiltersNormalCalls(t *testing.T) {
	runtime, _, _ := serveTestFixture(t)
	var stdout, stderr serveTestBuffer
	cancel, done := startServeTest(t, runtime, &stdout, &stderr, "--log-level", "minimal")
	awaitServeTest(t, func() bool { return strings.Contains(stdout.String(), "MCP 服务已启动") })
	runtime.History().Record("", "tool_call", "INFO", 2, "hidden-call")
	runtime.History().Record("", "tool_call", "ERROR", 1, "visible-error")
	awaitServeTest(t, func() bool { return strings.Contains(stderr.String(), "visible-error") })
	if err := stopServeTest(t, cancel, done); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String()+stderr.String(), "hidden-call") {
		t.Fatal("精简模式不应显示普通调用摘要")
	}
}

func TestMCPServeInvalidOptionsDoNotListen(t *testing.T) {
	for _, args := range [][]string{{"--log-level", "invalid"}, {"--color", "invalid"}, {"unexpected"}} {
		runtime, _, _ := serveTestFixture(t)
		command := newMCPServeCommand(Dependencies{MCP: runtime})
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("无效参数未拒绝: %v", args)
		}
		status, err := runtime.Status(context.Background())
		if err != nil || status.State != "stopped" {
			t.Fatal("参数错误启动了服务", err)
		}
	}
}

func TestMCPServeListenFailureReportedOnce(t *testing.T) {
	runtime, _, cfg := serveTestFixture(t)
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var stdout, stderr bytes.Buffer
	command := newMCPServeCommand(Dependencies{MCP: runtime})
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	err = command.Execute()
	if err == nil || !ErrorReported(err) {
		t.Fatalf("监听失败应返回已报告错误: %v", err)
	}
	if strings.Count(stderr.String(), "MCP 监听失败") != 1 || stdout.Len() != 0 {
		t.Fatalf("监听失败日志重复或流错误: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestMCPServeExistingInstanceReportsOwnerWithoutStoppingIt(t *testing.T) {
	owner, settings, cfg := serveTestFixture(t)
	if _, err := owner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	other := agentmcp.NewRuntime(settings, "cli", nil)
	var output bytes.Buffer
	command := newMCPServeCommand(Dependencies{MCP: other})
	command.SetOut(&output)
	command.SetErr(&output)
	if err := command.Execute(); err == nil || !ErrorReported(err) {
		t.Fatalf("重复启动应报告已有实例: %v", err)
	}
	if !strings.Contains(output.String(), cfg.URL()) || !strings.Contains(output.String(), "cli") {
		t.Fatalf("未显示已有实例的地址和宿主: %s", output.String())
	}
	status, err := owner.Status(context.Background())
	if err != nil || status.State != "running" {
		t.Fatal("重复启动停止了已有宿主", err)
	}
}

type serveTestFailWriter struct{}

func (serveTestFailWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestMCPServeOutputFailureStopsListener(t *testing.T) {
	runtime, _, _ := serveTestFixture(t)
	var stderr serveTestBuffer
	cancel, done := startServeTest(t, runtime, serveTestFailWriter{}, &stderr)
	defer cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("输出失败应返回错误")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("输出失败后没有退出")
	}
	status, err := runtime.Status(context.Background())
	if err != nil || status.State != "stopped" {
		t.Fatal("输出失败遗留监听", err)
	}
}

type serveTestBlockingWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *serveTestBlockingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestMCPServeSlowOutputDoesNotHoldHistoryOrPreventStop(t *testing.T) {
	runtime, _, _ := serveTestFixture(t)
	stdout := &serveTestBlockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(stdout.release)
	var stderr serveTestBuffer
	cancel, done := startServeTest(t, runtime, stdout, &stderr)
	select {
	case <-stdout.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("日志未进入输出")
	}
	recorded := make(chan struct{})
	go func() { runtime.History().Record("", "tool_call", "INFO", 2, "while-blocked"); close(recorded) }()
	select {
	case <-recorded:
	case <-time.After(time.Second):
		t.Fatal("阻塞输出持有 History 锁")
	}
	_ = stopServeTest(t, cancel, done)
	status, err := runtime.Status(context.Background())
	if err != nil || status.State != "stopped" {
		t.Fatal("输出堵塞时取消未停止服务", err)
	}
}

func TestMCPServeBlockedMergedOutputMustNotBeRetriedByMain(t *testing.T) {
	runtime, _, _ := serveTestFixture(t)
	output := &serveTestBlockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(output.release)
	cancel, done := startServeTest(t, runtime, output, output)
	select {
	case <-output.entered:
	case <-time.After(4 * time.Second):
		t.Fatal("日志未进入输出")
	}
	err := stopServeTest(t, cancel, done)
	if err == nil || !ErrorReported(err) {
		t.Fatalf("输出堵塞的错误必须由 serve 接管，不能让主入口再次同步写入: %v", err)
	}
}

type serveTestGateWriter struct {
	serveTestBlockingWriter
	buffer serveTestBuffer
}

func (w *serveTestGateWriter) Write(p []byte) (int, error) {
	_, _ = w.serveTestBlockingWriter.Write(p)
	return w.buffer.Write(p)
}

func TestMCPServeAcceptedFailureLineIsNotReplayedAfterCancel(t *testing.T) {
	history := agentmcp.NewHistory(4)
	history.Record("", "service_failed", "ERROR", 1, "host-failed")
	gate := &serveTestGateWriter{serveTestBlockingWriter: serveTestBlockingWriter{entered: make(chan struct{}), release: make(chan struct{})}}
	output := newMCPLogWriter(io.Discard, gate, false, false)
	defer output.close()
	reader := mcpLogReader{history: history, output: output, detail: 2}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- reader.flush(ctx) }()
	<-gate.entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("等待输出未被取消", err)
	}
	close(gate.release)
	if err := reader.report(errors.New("host-failed")); !ErrorReported(err) {
		t.Fatal("已入队错误未标记为已处理")
	}
	awaitServeTest(t, func() bool { return strings.Contains(gate.buffer.String(), "host-failed") })
	if strings.Count(gate.buffer.String(), "host-failed") != 1 {
		t.Fatal("取消与写入竞争导致同一服务错误重复输出")
	}
}

func TestMCPServeOnlyDeduplicatesTheSameFailure(t *testing.T) {
	history := agentmcp.NewHistory(4)
	history.Record("", "service_failed", "ERROR", 1, "host-failed")
	var stderr serveTestBuffer
	output := newMCPLogWriter(io.Discard, &stderr, false, false)
	defer output.close()
	reader := mcpLogReader{history: history, output: output, detail: 2}
	if err := reader.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = reader.report(errors.New("different-output-failure"))
	if !strings.Contains(stderr.String(), "different-output-failure") {
		t.Fatal("此前服务错误不应抑制新的不同诊断")
	}
}

func TestMCPServeHistoryGapIsReportedAndStreamsStayOrdered(t *testing.T) {
	history := agentmcp.NewHistory(2)
	var merged serveTestBuffer
	output := newMCPLogWriter(&merged, &merged, false, false)
	defer output.close()
	reader := mcpLogReader{history: history, output: output, detail: 2}
	history.Record("", "tool_call", "INFO", 2, "first-record")
	if err := reader.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	history.Record("", "tool_call", "INFO", 2, "evicted-record")
	history.Record("", "tool_call", "WARN", 1, "retained-warning")
	history.Record("", "tool_call", "INFO", 2, "retained-normal")
	for range 2 {
		if err := reader.flush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	text := merged.String()
	if strings.Count(text, "部分历史日志已回收") != 1 || strings.Contains(text, "evicted-record") || strings.Count(text, "first-record") != 1 {
		t.Fatalf("历史缺口或增量游标处理错误: %s", text)
	}
	if strings.Index(text, "retained-warning") > strings.Index(text, "retained-normal") {
		t.Fatal("两个流的写入顺序未遵守共享日志游标")
	}
}

func TestMCPServeAppliesColorToEachStreamIndependently(t *testing.T) {
	var stdout, stderr serveTestBuffer
	output := newMCPLogWriter(&stdout, &stderr, true, false)
	defer output.close()
	for _, severity := range []string{"INFO", "ERROR"} {
		if _, err := output.write(context.Background(), agentmcp.LogLine{Time: time.Now(), Severity: severity, Text: "message"}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(stdout.String(), "\x1b[32m") || strings.Contains(stderr.String(), "\x1b") {
		t.Fatal("stdout 与 stderr 未分别应用颜色能力")
	}
}

func TestMCPLogOutputIncludesLocalMonthAndDay(t *testing.T) {
	for _, ansi := range []bool{false, true} {
		var stdout, stderr serveTestBuffer
		output := newMCPLogWriter(&stdout, &stderr, ansi, ansi)
		defer output.close()
		for _, test := range []struct {
			year  int
			month time.Month
			day   int
			want  string
		}{
			{2026, time.January, 2, "01-02 03:04:05"},
			{2026, time.January, 3, "01-03 03:04:05"},
			{2026, time.December, 31, "12-31 03:04:05"},
			{2027, time.January, 1, "01-01 03:04:05"},
		} {
			stamp := time.Date(test.year, test.month, test.day, 3, 4, 5, 0, time.Local).UTC()
			for _, severity := range []string{"INFO", "ERROR"} {
				if _, err := output.write(context.Background(), agentmcp.LogLine{Time: stamp, Severity: severity, Text: "message"}); err != nil {
					t.Fatal(err)
				}
			}
			want := test.want
			if ansi {
				want = "\x1b[2m" + want + "\x1b[0m"
			}
			for _, text := range []string{stdout.String(), stderr.String()} {
				if !strings.Contains(text, want+"  ") {
					t.Fatalf("日志应包含本地月日和时间 %q（ANSI=%t）：%q", want, ansi, text)
				}
			}
		}
	}
}

func TestMCPServeKeepsRunningWhenSavedConfigCannotBeRead(t *testing.T) {
	runtime, settings, _ := serveTestFixture(t)
	var stdout, stderr serveTestBuffer
	cancel, done := startServeTest(t, runtime, &stdout, &stderr)
	awaitServeTest(t, func() bool { return strings.Contains(stdout.String(), "MCP 服务已启动") })
	if err := os.WriteFile(settings.Store.Path, []byte("invalid configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.History().Record("", "tool_call", "INFO", 2, "after-config-edit")
	awaitServeTest(t, func() bool { return strings.Contains(stdout.String(), "after-config-edit") })
	select {
	case err := <-done:
		t.Fatalf("已运行宿主依赖配置重新读取: %v", err)
	default:
	}
	if err := stopServeTest(t, cancel, done); err != nil {
		t.Fatal(err)
	}
}
