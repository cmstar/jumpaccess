package agentmcp

import (
	"context"
	"io"
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
	if terminal(job.view.Status) || job.exitReceived || job.finishing {
		return
	}
	if !job.view.CancelRequested {
		e.jobEventLocked(job, "cancel_requested", "INFO", 2, "已请求停止命令  "+job.view.ID, false)
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
	ready := job.dispatchReady
	remote := job.view.TimeoutStrategy == "remote_required"
	go func() {
		done := make(chan error, 1)
		go func() {
			select {
			case <-ready:
			case <-job.ctx.Done():
				done <- job.ctx.Err()
				return
			}
			e.mu.Lock()
			accepted := job.dispatchAccepted && !job.exitReceived && !terminal(job.view.Status) && job.ctx.Err() == nil
			e.mu.Unlock()
			if !accepted {
				done <- nil
				return
			}
			var err error
			if remote {
				_, err = io.WriteString(ch, stopMessage(job.marker))
			} else {
				_, err = ch.SendRequest("signal", false, ssh.Marshal(struct{ Signal string }{"TERM"}))
			}
			done <- err
		}()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		var err error
		invalidate := false
		select {
		case err = <-done:
		case <-timer.C:
			err, invalidate = context.DeadlineExceeded, true
		case <-job.ctx.Done():
			err, invalidate = job.ctx.Err(), true
		}
		if err != nil {
			e.mu.Lock()
			if !terminal(job.view.Status) && !job.exitReceived && !job.detached {
				job.view.CancelError = fault("CANCEL_DELIVERY_FAILED", "停止请求未能可靠送达，尚未确认远端结束")
				e.notifyExecution(job)
			}
			e.mu.Unlock()
		}
		if invalidate {
			e.invalidate(job.connection, client)
		}
		// 只等待 I/O 完成，不能让终态清理与停止确认互相等待。
		close(job.stopDone)
		e.awaitStopConfirmation(job)
	}()
	e.notifyExecution(job)
}

func (e *Engine) awaitStopConfirmation(job *execution) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-job.done:
		return
	case <-job.ctx.Done():
		return
	case <-timer.C:
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if terminal(job.view.Status) || job.exitReceived || job.ctx.Err() != nil {
		return
	}
	if job.view.CancelError == nil {
		job.view.CancelError = fault("CANCEL_UNCONFIRMED", "停止请求发出后未收到远端结束确认，结果未知")
	}
	job.stopUnconfirmed = true
	// 只取消原任务；由其收集器清理捕获的连接，不能误伤随后复用的任务。
	job.cancel()
	e.notifyExecution(job)
}
