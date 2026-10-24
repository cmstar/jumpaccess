package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/spf13/cobra"
)

const (
	mcpLogPollInterval = 100 * time.Millisecond
	mcpShutdownTimeout = 5 * time.Second
	mcpOutputTimeout   = time.Second
)

type reportedMCPError struct{ err error }

func (e *reportedMCPError) Error() string { return e.err.Error() }
func (e *reportedMCPError) Unwrap() error { return e.err }

// ErrorReported 表示命令已接管诊断输出，进程入口不得再次同步打印。
// 输出流不可用或写入超时也由命令处理，以免进程入口重新阻塞退出。
func ErrorReported(err error) bool {
	var reported *reportedMCPError
	return errors.As(err, &reported)
}

func newMCPServeCommand(deps Dependencies) *cobra.Command {
	level, color := "standard", "auto"
	command := &cobra.Command{
		Use:           "serve",
		Short:         "前台运行 MCP 服务并持续输出日志",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			detail, ok := map[string]int{"minimal": 1, "standard": 2, "detailed": 3}[level]
			if !ok {
				return fmt.Errorf("日志详细程度必须为 minimal、standard 或 detailed")
			}
			if color != "auto" && color != "always" && color != "never" {
				return fmt.Errorf("颜色模式必须为 auto、always 或 never")
			}
			if deps.MCP == nil {
				return fmt.Errorf("MCP 服务未装配")
			}
			stdoutColor, restoreStdout := prepareMCPColor(cmd.OutOrStdout(), color)
			defer restoreStdout()
			stderrColor, restoreStderr := prepareMCPColor(cmd.ErrOrStderr(), color)
			defer restoreStderr()
			output := newMCPLogWriter(cmd.OutOrStdout(), cmd.ErrOrStderr(), stdoutColor, stderrColor)
			defer output.close()
			ctx, stopSignals := mcpServeContext(cmd.Context())
			defer stopSignals()
			return serveMCP(ctx, deps.MCP, output, detail)
		},
	}
	command.Flags().StringVar(&level, "log-level", "standard", "日志详细程度：minimal、standard、detailed")
	command.Flags().StringVar(&color, "color", "auto", "ANSI 颜色：auto、always、never")
	return command
}

func serveMCP(ctx context.Context, runtime *agentmcp.Runtime, output *mcpLogWriter, detail int) error {
	reader := mcpLogReader{history: runtime.History(), output: output, detail: detail}
	if _, err := runtime.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, agentmcp.ErrRunning) {
			if status, statusErr := runtime.Status(ctx); statusErr == nil && status.Instance != nil {
				err = fmt.Errorf("%w；现有实例地址=%s，宿主=%s", err, status.Instance.URL, status.Instance.Host)
			}
		}
		flushCtx, cancel := context.WithTimeout(context.Background(), mcpOutputTimeout)
		_ = reader.flush(flushCtx)
		cancel()
		return reader.report(err)
	}

	// Runtime 的生存期等待不重新读取配置。写入输出流阻塞时，取消或宿主故障
	// 仍可唤醒前台循环，停止监听和释放服务资源。
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	waitCtx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- runtime.Wait(waitCtx)
		cancelRun()
	}()
	ticker := time.NewTicker(mcpLogPollInterval)
	defer ticker.Stop()
	var failure, waitErr error
	waitFinished := false

running:
	for {
		if err := reader.flush(runCtx); err != nil {
			if runCtx.Err() == nil {
				failure = fmt.Errorf("MCP 日志输出失败: %w", err)
			}
			break
		}
		select {
		case waitErr = <-waitDone:
			waitFinished = true
			break running
		case <-ctx.Done():
			break running
		case <-ticker.C:
		}
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), mcpShutdownTimeout)
	stopErr := runtime.Stop(stopCtx)
	cancelStop()
	cancelWait()
	if !waitFinished {
		waitErr = <-waitDone
	}
	if failure == nil && waitErr != nil && !errors.Is(waitErr, context.Canceled) && !errors.Is(waitErr, context.DeadlineExceeded) {
		failure = waitErr
	}
	if failure == nil && stopErr != nil {
		failure = fmt.Errorf("MCP 服务停止失败: %w", stopErr)
	}

	flushCtx, cancelFlush := context.WithTimeout(context.Background(), mcpOutputTimeout)
	flushErr := reader.flush(flushCtx)
	cancelFlush()
	if failure == nil && flushErr != nil {
		failure = fmt.Errorf("MCP 服务已停止，日志输出未完成: %w", flushErr)
	}
	if failure != nil {
		return reader.report(failure)
	}
	return nil
}

type mcpLogReader struct {
	history            *agentmcp.History
	output             *mcpLogWriter
	detail             int
	cursor             uint64
	gapCursor          uint64
	serviceFailureText string
}

func (r *mcpLogReader) flush(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page := r.history.Read(r.cursor, 200)
		if page.Gap && r.gapCursor != r.cursor {
			accepted, err := r.output.write(ctx, agentmcp.LogLine{Time: time.Now(), Severity: "WARN", Text: "部分历史日志已回收，继续输出仍保留的记录。"})
			if accepted {
				r.gapCursor = r.cursor
			}
			if err != nil {
				return err
			}
		}
		for _, line := range page.Lines {
			if line.Detail > r.detail {
				r.cursor = line.Sequence
				continue
			}
			accepted, err := r.output.write(ctx, line)
			if accepted {
				// 写请求已进入唯一写协程。取消后不能重新发送这一行，
				// 原写入可能正在阻塞或即将完成。
				r.cursor = line.Sequence
				if line.Event == "service_failed" && line.Severity == "ERROR" {
					r.serviceFailureText = line.Text
				}
			}
			if err != nil {
				return err
			}
		}
		r.cursor = page.NextCursor
		if !page.HasMore {
			return nil
		}
	}
}

func (r *mcpLogReader) report(err error) error {
	if r.serviceFailureText != "" && (err.Error() == r.serviceFailureText || strings.HasPrefix(err.Error(), r.serviceFailureText+": ")) {
		return &reportedMCPError{err: err}
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpOutputTimeout)
	defer cancel()
	_, _ = r.output.write(ctx, agentmcp.LogLine{Time: time.Now(), Severity: "ERROR", Text: err.Error()})
	return &reportedMCPError{err: err}
}

type mcpLogWrite struct {
	destination io.Writer
	text        string
	result      chan error
}

// 一个写协程最多持有一个正在写入的整行，不为慢终端复制无限日志。
// 任意 io.Writer 不保证支持取消；等待写入可以取消，宿主退出不等待阻塞的写系统调用。
type mcpLogWriter struct {
	stdout, stderr           io.Writer
	stdoutColor, stderrColor bool
	writes                   chan mcpLogWrite
	closed                   chan struct{}
	once                     sync.Once
}

func newMCPLogWriter(stdout, stderr io.Writer, stdoutColor, stderrColor bool) *mcpLogWriter {
	w := &mcpLogWriter{stdout: stdout, stderr: stderr, stdoutColor: stdoutColor, stderrColor: stderrColor, writes: make(chan mcpLogWrite), closed: make(chan struct{})}
	go func() {
		for {
			select {
			case <-w.closed:
				return
			case request := <-w.writes:
				n, err := io.WriteString(request.destination, request.text)
				if err == nil && n != len(request.text) {
					err = io.ErrShortWrite
				}
				request.result <- err
			}
		}
	}()
	return w
}

func (w *mcpLogWriter) close() { w.once.Do(func() { close(w.closed) }) }

func (w *mcpLogWriter) write(ctx context.Context, line agentmcp.LogLine) (bool, error) {
	destination, color := w.stdout, w.stdoutColor
	if line.Severity == "WARN" || line.Severity == "ERROR" {
		destination, color = w.stderr, w.stderrColor
	}
	request := mcpLogWrite{destination: destination, text: agentmcp.FormatLogLine(line, color) + "\n", result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-w.closed:
		return false, io.ErrClosedPipe
	case w.writes <- request:
	}
	select {
	case err := <-request.result:
		return true, err
	case <-ctx.Done():
		return true, ctx.Err()
	}
}
