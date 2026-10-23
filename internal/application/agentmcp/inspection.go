package agentmcp

import "context"

type EngineStats struct {
	Connections            int  `json:"connections"`
	SSHConnections         int  `json:"ssh_connections"`
	Connecting             int  `json:"connecting"`
	Executions             int  `json:"executions"`
	Downloads              int  `json:"downloads"`
	UnknownDownloads       int  `json:"unknown_downloads"`
	DownloadTransportSlots int  `json:"download_transport_slots"`
	UnknownExecutions      int  `json:"unknown_executions"`
	Draining               bool `json:"draining"`
}

func (e *Engine) Stats() EngineStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := EngineStats{Draining: e.closing}
	s.DownloadTransportSlots = e.downloadTransports
	for _, job := range e.downloads {
		if !terminal(job.view.Status) {
			s.Downloads++
		}
		if job.view.Status == "unknown" {
			s.UnknownDownloads++
		}
	}
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
	return map[string]int64{
		"connections": int64(l.Connections), "ssh_connections": int64(l.Transports), "operation_records": int64(e.history.limit), "execution_records": int64(l.Executions), "request_history": int64(l.Requests), "active_requests": int64(l.ActiveRequests), "input_bytes": 16 << 20, "output_bytes": int64(l.OutputBytes), "total_output_bytes": int64(l.TotalOutputBytes), "ssh_idle_ms": l.IdleTimeout.Milliseconds(), "history_ttl_ms": l.HistoryTTL.Milliseconds(), "default_timeout_ms": 300000, "max_timeout_ms": 3600000, "max_wait_ms": 5000,
		"active_downloads": 4, "download_paths": 100, "download_entries": 1000, "download_metadata_bytes": 64 << 20, "download_default_timeout_ms": 1800000, "download_max_timeout_ms": 3600000, "download_start_max_wait_ms": 5000, "download_query_default_wait_ms": 20000, "download_query_max_wait_ms": 30000, "download_capability_cache_entries": 256, "download_capability_cache_ttl_ms": 600000,
		"download_transport_max_bytes": 1 << 30, "download_cache_bytes": 8 << 30, "download_cache_files": 50, "download_link_ttl_ms": 1800000, "download_http_concurrency": 4, "download_http_per_file": 2, "download_http_idle_timeout_ms": 60000, "download_http_total_timeout_ms": 900000,
	}
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
