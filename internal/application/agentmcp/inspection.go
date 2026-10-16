package agentmcp

import "context"

type EngineStats struct {
	Connections       int  `json:"connections"`
	SSHConnections    int  `json:"ssh_connections"`
	Connecting        int  `json:"connecting"`
	Executions        int  `json:"executions"`
	UnknownExecutions int  `json:"unknown_executions"`
	Draining          bool `json:"draining"`
}

func (e *Engine) Stats() EngineStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := EngineStats{Draining: e.closing}
	for _, c := range e.connections {
		if c.view.Status != "closed" {
			s.Connections++
		}
		if c.client != nil {
			s.SSHConnections++
		}
		if c.dial != nil {
			s.Connecting++
		}
	}
	for _, job := range e.executions {
		if !terminal(job.view.Status) {
			s.Executions++
		}
		if job.view.Status == "unknown" {
			s.UnknownExecutions++
		}
	}
	return s
}
func (e *Engine) Limits() map[string]int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	l := e.opts.Limits
	return map[string]int64{"connections": int64(l.Connections), "ssh_connections": int64(l.Transports), "execution_records": int64(l.Executions), "request_history": int64(l.Requests), "active_requests": int64(l.ActiveRequests), "output_bytes": int64(l.OutputBytes), "total_output_bytes": int64(l.TotalOutputBytes), "ssh_idle_ms": l.IdleTimeout.Milliseconds(), "history_ttl_ms": l.HistoryTTL.Milliseconds(), "default_timeout_ms": 300000, "max_timeout_ms": 3600000, "max_wait_ms": 5000}
}
func (e *Engine) ConnectionsPage(ctx context.Context, in PageInput) (Page[ConnectionView], error) {
	offset, limit, key, err := e.pages.page(in, "connections")
	if err != nil {
		return Page[ConnectionView]{}, err
	}
	all, err := e.ListConnections(ctx, struct{}{})
	if err != nil {
		return Page[ConnectionView]{}, err
	}
	return pageResult(e.pages, window(all, offset, limit), len(all), offset, key, "", ""), nil
}

type ExecutionsPageInput struct {
	PageInput
	ListExecutionsInput
}

func (e *Engine) ExecutionsPage(ctx context.Context, in ExecutionsPageInput) (Page[ExecutionView], error) {
	if in.Status != "" && !terminal(in.Status) && in.Status != "starting" && in.Status != "running" && in.Status != "cancel_requested" {
		return Page[ExecutionView]{}, fault("INVALID_ARGUMENT", "执行状态无效")
	}
	offset, limit, key, err := e.pages.page(in.PageInput, "executions", in.ConnectionID, in.Status)
	if err != nil {
		return Page[ExecutionView]{}, err
	}
	all, err := e.ListExecutions(ctx, in.ListExecutionsInput)
	if err != nil {
		return Page[ExecutionView]{}, err
	}
	return pageResult(e.pages, window(all, offset, limit), len(all), offset, key, "", ""), nil
}
