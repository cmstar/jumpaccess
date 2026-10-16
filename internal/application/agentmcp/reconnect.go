package agentmcp

import (
	"context"
	"time"

	"golang.org/x/crypto/ssh"
)

func (e *Engine) prepareTransport(ctx context.Context, job *execution) (*ssh.Client, uint64, error) {
	e.mu.Lock()
	previous := job.connection.view.TransportGeneration
	e.mu.Unlock()
	client, generation, attempted, err := e.transport(ctx, job.connection, true)
	e.mu.Lock()
	if attempted {
		job.view.ConnectAttempts++
	}
	job.view.Reconnected = job.view.Reconnected || (previous > 0 && generation > previous)
	e.mu.Unlock()
	if err != nil {
		return nil, 0, err
	}
	if job.input.TimeoutStrategy == "remote_required" {
		if err := e.probeDeadline(ctx, job, client); err != nil {
			e.mu.Lock()
			retry := job.connection.client == nil && job.view.ConnectAttempts == 0 && !job.connection.closing
			e.mu.Unlock()
			if retry && ctx.Err() == nil {
				return e.prepareTransport(ctx, job)
			}
			return nil, 0, err
		}
	}
	if err := e.opts.Validate(ctx, job.view.Target); err != nil {
		return nil, 0, err
	}
	return client, generation, nil
}

// 清理结束前不释放执行位置。底层写阻塞时只关闭本次捕获的 transport。
func (e *Engine) closeChannel(ch ssh.Channel, client *ssh.Client, c *connection) {
	done := make(chan struct{})
	go func() { _ = ch.Close(); close(done) }()
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		e.invalidate(c, client)
	}
}
