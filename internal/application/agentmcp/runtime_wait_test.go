package agentmcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/config"
)

func waitRuntimeFixture(t *testing.T) *Runtime {
	t.Helper()
	settings := testSettings(t)
	cfg := config.DefaultMCP()
	cfg.Token, _ = settings.GenerateToken(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err := settings.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(settings, "cli", func(Instance, config.MCP, string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	if _, err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Stop(ctx)
	})
	return runtime
}

func TestRuntimeWaitTracksListenerWithoutReadingSavedConfig(t *testing.T) {
	runtime := waitRuntimeFixture(t)
	if err := os.WriteFile(runtime.settings.Store.Path, []byte("invalid = ["), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.Wait(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("运行中Wait提前退出: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	stop, finish := context.WithTimeout(context.Background(), time.Second)
	defer finish()
	if err := runtime.Stop(stop); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop后Wait未返回")
	}
}

func TestRuntimeWaitReportsUnexpectedListenerExit(t *testing.T) {
	runtime := waitRuntimeFixture(t)
	runtime.mu.Lock()
	server := runtime.active.server
	runtime.mu.Unlock()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Wait(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("意外停止未报告: %v", err)
	}
}
