//go:build !windows

package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func mcpServeContext(parent context.Context) (context.Context, context.CancelFunc) {
	// 标准输出断管时让 Write 返回 EPIPE，交给日志输出错误路径停止服务。
	// 不用此信号取消上下文，以免把输出故障误判为正常 Ctrl+C 退出。
	brokenPipe := make(chan os.Signal, 1)
	signal.Notify(brokenPipe, syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	return ctx, func() {
		stop()
		signal.Stop(brokenPipe)
	}
}
