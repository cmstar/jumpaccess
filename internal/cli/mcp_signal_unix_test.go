//go:build !windows

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

type mcpPipeTestHandler struct{ directory string }

func (mcpPipeTestHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
func (h mcpPipeTestHandler) Shutdown(context.Context) error {
	return os.WriteFile(filepath.Join(h.directory, "shutdown"), nil, 0o600)
}
func (mcpPipeTestHandler) Stats() agentmcp.EngineStats { return agentmcp.EngineStats{} }

func TestMCPServeBrokenStandardPipeCleansUp(t *testing.T) {
	if directory := os.Getenv("JUMPACCESS_MCP_PIPE_TEST_DIR"); directory != "" {
		runMCPBrokenPipeChild(t, directory)
		return
	}
	for _, stream := range []string{"stdout", "stderr"} {
		for _, verifyRestore := range []bool{false, true} {
			name := stream + "/cleanup"
			if verifyRestore {
				name = stream + "/restore"
			}
			t.Run(name, func(t *testing.T) {
				directory := t.TempDir()
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				// 子进程的真实 fd 1/2 写入无读取端的管道；普通 io.Writer 替身不会触发 SIGPIPE。
				reader.Close()
				defer writer.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPServeBrokenStandardPipeCleansUp$")
				command.Env = append(os.Environ(), "JUMPACCESS_MCP_PIPE_TEST_DIR="+directory, "JUMPACCESS_MCP_PIPE_TEST_STREAM="+stream)
				if verifyRestore {
					command.Env = append(command.Env, "JUMPACCESS_MCP_PIPE_TEST_RESTORE=1")
				}
				var diagnostic bytes.Buffer
				command.Stdout, command.Stderr = &diagnostic, &diagnostic
				if stream == "stdout" {
					command.Stdout = writer
				} else {
					command.Stderr = writer
				}
				err = command.Run()
				if ctx.Err() != nil {
					t.Fatalf("断管后宿主未及时退出: %v", err)
				}
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("断管未返回失败退出码: %v; output=%s", err, diagnostic.String())
				}
				status := exit.Sys().(syscall.WaitStatus)
				if verifyRestore {
					if !status.Signaled() || status.Signal() != syscall.SIGPIPE {
						t.Fatalf("serve 返回后未恢复 SIGPIPE 行为: %v; output=%s", err, diagnostic.String())
					}
				} else if exit.ExitCode() != 1 {
					t.Fatalf("应由 serve 报错退出，不能被 SIGPIPE 提前终止: %v; output=%s", err, diagnostic.String())
				}
				for _, marker := range []string{"shutdown", "returned"} {
					if _, err := os.Stat(filepath.Join(directory, marker)); err != nil {
						t.Fatalf("serve 未完成 %s: %v; output=%s", marker, err, diagnostic.String())
					}
				}
				if _, err := os.Stat(filepath.Join(directory, "mcp-instance.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("断管后未清理实例描述: %v", err)
				}
			})
		}
	}
}

func runMCPBrokenPipeChild(t *testing.T, directory string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultMCP()
	cfg.Port, cfg.Token = listener.Addr().(*net.TCPAddr).Port, strings.Repeat("A", 43)
	listener.Close()
	settings := agentmcp.Settings{Store: config.Store{Path: filepath.Join(directory, "config.toml")}, Locker: filelock.Locker{Dir: filepath.Join(directory, "locks")}}
	if err := settings.SaveConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	var runtime *agentmcp.Runtime
	runtime = agentmcp.NewRuntime(settings, "cli", func(agentmcp.Instance, config.MCP, string) http.Handler {
		if os.Getenv("JUMPACCESS_MCP_PIPE_TEST_STREAM") == "stderr" {
			runtime.History().Record("", "tool_call", "ERROR", 1, "broken-pipe-test")
		}
		return mcpPipeTestHandler{directory: directory}
	})
	command := newMCPServeCommand(Dependencies{MCP: runtime})
	command.SetOut(os.Stdout)
	command.SetErr(os.Stderr)
	if err := command.Execute(); err == nil || !ErrorReported(err) {
		os.Exit(11)
	}
	if err := os.WriteFile(filepath.Join(directory, "returned"), nil, 0o600); err != nil {
		os.Exit(12)
	}
	if os.Getenv("JUMPACCESS_MCP_PIPE_TEST_RESTORE") != "" {
		stream := os.Stdout
		if os.Getenv("JUMPACCESS_MCP_PIPE_TEST_STREAM") == "stderr" {
			stream = os.Stderr
		}
		_, _ = io.WriteString(stream, "after-serve\n")
		os.Exit(13)
	}
	// 模拟 main 收到已报告错误后的退出，避免 testing 再次写入已经断开的 stdout。
	os.Exit(1)
}
