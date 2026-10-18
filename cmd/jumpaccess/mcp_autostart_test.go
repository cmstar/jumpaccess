package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
)

func autoStartApp(t *testing.T, enabled bool) *desktopApp {
	t.Helper()
	root := t.TempDir()
	prefs, err := guiconfig.Decode([]byte("[mcp]\nauto_start = " + strconv.FormatBool(enabled)))
	if err != nil {
		t.Fatal(err)
	}
	if err := (guiconfig.Store{Path: filepath.Join(root, "gui.toml")}).Save(prefs); err != nil {
		t.Fatal(err)
	}
	app, err := newDesktopApp(root)
	if err != nil {
		t.Fatal(err)
	}
	app.superviseAuth = nil
	t.Cleanup(func() { app.shutdown(context.Background()) })
	return app
}

func configureAutoStartMCP(t *testing.T, app *desktopApp) config.MCP {
	t.Helper()
	cfg, err := app.core.MCPSettings.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Token = strings.Repeat("A", 43)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if _, err := app.SaveMCPConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func awaitMCPEvent(t *testing.T, app *desktopApp, event string) agentmcp.LogLine {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, line := range app.mcp.History().Read(0, 200).Lines {
			if line.Event == event {
				return line
			}
		}
		select {
		case <-deadline:
			t.Fatalf("未收到 %s", event)
		case <-ticker.C:
		}
	}
}

func TestMCPAutoStartRunsInBackgroundOnce(t *testing.T) {
	app := autoStartApp(t, true)
	configureAutoStartMCP(t, app)
	entered, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int32
	app.mcp = agentmcp.NewRuntime(app.core.MCPSettings, "gui", func(agentmcp.Instance, config.MCP, string) http.Handler {
		if attempts.Add(1) == 1 {
			close(entered)
		}
		<-release
		return http.NotFoundHandler()
	})
	defer close(release)
	returned := make(chan struct{})
	go func() { app.startup(context.Background()); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("自动启动阻塞了应用启动")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("未尝试自动启动")
	}
	// 后台仍在准备 MCP 时，其余桌面能力可用，重复启动回调也不安排第二次尝试。
	if _, err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	app.startup(context.Background())
	if attempts.Load() != 1 {
		t.Fatal("重复启动")
	}
}

func TestMCPAutoStartFailureIsLoggedAndManualRetryWorks(t *testing.T) {
	app := autoStartApp(t, true)
	app.startup(context.Background())
	line := awaitMCPEvent(t, app, "auto_start_failed")
	if line.Severity != "ERROR" || line.Detail != 1 {
		t.Fatal("自动启动失败须在所有日志级别可见")
	}
	configureAutoStartMCP(t, app)
	status, err := app.StartMCP()
	if err != nil || status.State != "running" {
		t.Fatal("失败后无法手动重试", err)
	}
	app.shutdown(context.Background())
	status, err = app.GetMCPStatus()
	if err != nil || status.State != "stopped" {
		t.Fatal("退出未停止服务", err)
	}
}

func TestMCPManualStopOrShutdownSuppressesPendingAutoStart(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		app := autoStartApp(t, true)
		configureAutoStartMCP(t, app)
		if shutdown {
			app.shutdown(context.Background())
		} else if _, err := app.StopMCP(); err != nil {
			t.Fatal(err)
		}
		app.startup(context.Background())
		time.Sleep(50 * time.Millisecond)
		status, err := app.GetMCPStatus()
		if err != nil || status.State != "stopped" {
			t.Fatal("后台任务在停止或退出之后又启动了服务", err)
		}
	}
}

func TestMCPAutoStartPreferenceOnlyAffectsNextLaunch(t *testing.T) {
	app := autoStartApp(t, false)
	configureAutoStartMCP(t, app)
	app.startup(context.Background())
	before, err := os.ReadFile(app.core.MCPSettings.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := guiconfig.Decode([]byte("[mcp]\nauto_start = true"))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	status, err := app.GetMCPStatus()
	if err != nil || status.State != "stopped" {
		t.Fatal("偏好保存不应立即启动服务", err)
	}
	after, err := os.ReadFile(app.core.MCPSettings.Store.Path)
	if err != nil || string(before) != string(after) {
		t.Fatal("GUI 偏好污染了共享配置", err)
	}
	reopened, err := newDesktopApp(filepath.Dir(app.preferences.Path))
	if err != nil {
		t.Fatal(err)
	}
	reopened.superviseAuth = nil
	t.Cleanup(func() { reopened.shutdown(context.Background()) })
	reopened.startup(context.Background())
	awaitMCPEvent(t, reopened, "service_started")
	prefs.MCP.AutoStart = false
	if err := reopened.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	status, err = reopened.GetMCPStatus()
	if err != nil || status.State != "running" {
		t.Fatal("关闭自动启动不应停止当前服务", err)
	}
}

func TestMCPAutoStartPortConflictDoesNotRetry(t *testing.T) {
	app := autoStartApp(t, true)
	cfg := configureAutoStartMCP(t, app)
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	app.startup(context.Background())
	line := awaitMCPEvent(t, app, "auto_start_failed")
	if strings.Contains(line.Text, cfg.Token) {
		t.Fatal("自动启动日志泄漏了 Token")
	}
	listener.Close()
	app.startup(context.Background())
	status, err := app.GetMCPStatus()
	if err != nil || status.State != "failed" {
		t.Fatal("自动重试覆盖了启动失败状态", err)
	}
	status, err = app.StartMCP()
	if err != nil || status.State != "running" {
		t.Fatal("释放端口后无法手动启动", err)
	}
}

func TestMCPAutoStartDoesNotTakeOverOtherHost(t *testing.T) {
	app := autoStartApp(t, true)
	configureAutoStartMCP(t, app)
	other := agentmcp.NewRuntime(app.core.MCPSettings, "gui", func(agentmcp.Instance, config.MCP, string) http.Handler { return http.NotFoundHandler() })
	started, err := other.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Stop(context.Background())
	app.startup(context.Background())
	line := awaitMCPEvent(t, app, "auto_start_failed")
	if !strings.Contains(line.Text, "其他进程") {
		t.Fatal("没有说明已有实例占用")
	}
	app.shutdown(context.Background())
	status, err := other.Status(context.Background())
	if err != nil || status.State != "running" || status.Instance.ID != started.Instance.ID {
		t.Fatal("退出影响了其他宿主", err)
	}
}

func TestMCPShutdownDuringAutoStartLeavesNoListener(t *testing.T) {
	app := autoStartApp(t, true)
	cfg := configureAutoStartMCP(t, app)
	entered, release := make(chan struct{}), make(chan struct{})
	app.mcp = agentmcp.NewRuntime(app.core.MCPSettings, "gui", func(agentmcp.Instance, config.MCP, string) http.Handler {
		close(entered)
		<-release
		return http.NotFoundHandler()
	})
	app.startup(context.Background())
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("后台启动未就绪")
	}
	finished := make(chan struct{})
	go func() { app.shutdown(context.Background()); close(finished) }()
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("退出等待未结束")
	}
	status, err := app.GetMCPStatus()
	if err != nil || status.State != "stopped" {
		t.Fatal("退出后还在运行", err)
	}
	listener, err := net.Listen("tcp", cfg.Address())
	if err != nil {
		t.Fatal("退出后监听端口仍被占用", err)
	}
	listener.Close()
}
