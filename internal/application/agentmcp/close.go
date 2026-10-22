package agentmcp

import (
	"context"
	"time"
)

func (e *Engine) beginCloseLocked(c *connection, force bool) {
	if c.view.Status == "closed" {
		return
	}
	c.forceClose = c.forceClose || force
	if c.closing {
		return
	}
	c.closing = true
	c.view.Status = "draining"
	c.closeDone = make(chan struct{})
	if c.dial != nil && (force || c.view.ActiveExecutionID == "") {
		c.dial.cancel()
	}
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			e.mu.Lock()
			if c.view.Status == "closed" {
				e.mu.Unlock()
				return
			}
			job := e.executions[c.view.ActiveExecutionID]
			if job == nil && c.refs == 0 && c.dial == nil {
				e.closeLocked(c, "explicit")
				e.mu.Unlock()
				return
			}
			if job != nil && c.forceClose {
				e.cancelLocked(job, time.Now().After(deadline))
			}
			if c.forceClose && time.Now().After(deadline) {
				e.closeLocked(c, "force")
				e.mu.Unlock()
				return
			}
			e.mu.Unlock()
			select {
			case <-ticker.C:
			case <-e.ctx.Done():
				e.mu.Lock()
				e.closeLocked(c, "service_stop")
				e.mu.Unlock()
				return
			}
		}
	}()
}

// Shutdown 请求停止共享任务，并在同一截止时间内等待；超时只能放弃跟踪，不能宣称远端已停止。
func (e *Engine) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e.mu.Lock()
	e.closing = true
	jobs := make([]*execution, 0)
	downloadJobs := make([]*download, 0)
	for _, job := range e.downloads {
		if !terminal(job.view.Status) {
			job.view.Status = "cancelling"
			job.cancel()
			downloadJobs = append(downloadJobs, job)
		}
	}
	for _, job := range e.executions {
		if !terminal(job.view.Status) {
			jobs = append(jobs, job)
			e.cancelLocked(job, false)
		}
	}
	e.mu.Unlock()
	var err error
	for _, job := range downloadJobs {
		select {
		case <-job.done:
		case <-ctx.Done():
			err = ctx.Err()
		}
		if err != nil {
			break
		}
	}
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-ctx.Done():
			err = ctx.Err()
		}
		if err != nil {
			break
		}
	}
	e.mu.Lock()
	for _, job := range downloadJobs {
		if !terminal(job.view.Status) {
			if job.view.Directory != "" {
				job.view.Warnings = append(job.view.Warnings, "本地文件已完成；服务停止前未能确认远端临时文件清理结果")
				e.finishDownloadLocked(job, "completed")
				continue
			}
			job.view.Error = fault("REMOTE_STATE_UNKNOWN", "服务停止，未能确认远端下载准备进程已结束")
			job.view.RemoteMayContinue = true
			e.finishDownloadLocked(job, "unknown")
		}
	}
	for _, job := range jobs {
		if !terminal(job.view.Status) {
			e.cancelLocked(job, true)
		}
	}
	e.cancel()
	for _, c := range e.connections {
		e.closeLocked(c, "service_stop")
	}
	e.mu.Unlock()
	return err
}
