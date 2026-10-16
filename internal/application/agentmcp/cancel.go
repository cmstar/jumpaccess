package agentmcp

import (
	"context"
	"time"

	"golang.org/x/crypto/ssh"
)

type CancelInput struct {
	ExecutionID string `json:"execution_id"`
	Mode        string `json:"mode,omitempty"`
}

func (e *Engine) CancelExecution(ctx context.Context, in CancelInput) (ExecutionView, error) {
	if in.Mode != "" && in.Mode != "request_stop" && in.Mode != "detach" {
		return ExecutionView{}, fault("INVALID_ARGUMENT", "取消模式必须是 request_stop 或 detach")
	}
	e.mu.Lock()
	if err := e.checkID(in.ExecutionID); err != nil {
		e.mu.Unlock()
		return ExecutionView{}, err
	}
	job := e.executions[in.ExecutionID]
	if job == nil {
		e.mu.Unlock()
		return ExecutionView{}, fault("EXECUTION_NOT_FOUND", "执行不存在或历史已回收")
	}
	e.cancelLocked(job, in.Mode == "detach")
	e.mu.Unlock()
	e.waitExecution(ctx, job, 1000, false)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executionView(job, 0, 64<<10), nil
}
func (e *Engine) cancelLocked(job *execution, detach bool) {
	if terminal(job.view.Status) || job.exitReceived {
		return
	}
	job.view.CancelRequested = true
	if job.view.DispatchState == "not_sent" {
		job.cancel()
		e.notifyExecution(job)
		return
	}
	job.view.Status = "cancel_requested"
	if detach {
		job.detached = true
		job.cancel()
		e.notifyExecution(job)
		return
	}
	if job.stopDone != nil {
		return
	}
	job.stopDone = make(chan struct{})
	ch, client := job.channel, job.client
	go func() {
		done := make(chan struct{})
		go func() {
			_, _ = ch.SendRequest("signal", false, ssh.Marshal(struct{ Signal string }{"TERM"}))
			close(done)
		}()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			e.invalidate(job.connection, client)
		case <-job.ctx.Done():
			e.invalidate(job.connection, client)
		}
		close(job.stopDone)
	}()
	e.notifyExecution(job)
}
