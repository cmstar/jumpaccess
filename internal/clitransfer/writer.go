package clitransfer

import (
	"context"
	"io"
	"sync"
)

type writeResult struct {
	n   int
	err error
}
type writeRequest struct {
	ctx  context.Context
	data []byte
	done chan writeResult
}
type remoteWriter struct {
	ctx       context.Context
	out       io.Writer
	queue     chan writeRequest
	closeOnce sync.Once
}

// 所有远端写入串行化；传输取消时若写操作被远端流控卡住，关闭损坏的会话解锁。
func newRemoteWriter(ctx context.Context, out io.Writer) *remoteWriter {
	w := &remoteWriter{ctx: ctx, out: out, queue: make(chan writeRequest)}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case request := <-w.queue:
				if err := request.ctx.Err(); err != nil {
					request.done <- writeResult{err: err}
					continue
				}
				n, err := out.Write(request.data)
				request.done <- writeResult{n, err}
			}
		}
	}()
	return w
}
func (w *remoteWriter) Write(p []byte) (int, error) { return w.write(w.ctx, p) }
func (w *remoteWriter) CloseInput() error {
	if closer, ok := w.out.(interface{ CloseInput() error }); ok {
		return closer.CloseInput()
	}
	return nil
}
func (w *remoteWriter) write(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	request := writeRequest{ctx: ctx, data: append([]byte(nil), p...), done: make(chan writeResult, 1)}
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	case w.queue <- request:
	}
	select {
	case result := <-request.done:
		return result.n, result.err
	case <-ctx.Done():
		select {
		case result := <-request.done:
			return result.n, result.err
		default:
		}
		w.closeOnce.Do(func() {
			if closer, ok := w.out.(io.Closer); ok {
				_ = closer.Close()
			}
		})
		return 0, ctx.Err()
	case <-w.ctx.Done():
		w.closeOnce.Do(func() {
			if closer, ok := w.out.(io.Closer); ok {
				_ = closer.Close()
			}
		})
		return 0, w.ctx.Err()
	}
}
