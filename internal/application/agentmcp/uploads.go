package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type UploadInput struct {
	Target          Target   `json:"target,omitempty"`
	ConnectionID    string   `json:"connection_id,omitempty"`
	RequestKey      string   `json:"request_key"`
	Source          string   `json:"source"`
	LocalPaths      []string `json:"local_paths"`
	RemoteDirectory string   `json:"remote_directory"`
	TimeoutMS       int      `json:"timeout_ms,omitempty"`
	WaitMS          *int     `json:"wait_ms,omitempty"`
}
type UploadIDInput struct {
	UploadID string `json:"upload_id"`
}
type GetUploadInput struct {
	UploadID string `json:"upload_id"`
	WaitMS   *int   `json:"wait_ms,omitempty"`
}
type UploadsPageInput struct {
	PageInput
	Status string `json:"status,omitempty"`
}
type UploadedFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Source    string `json:"source,omitempty"`
	Size      int64  `json:"size"`
	Directory bool   `json:"directory"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}
type UploadProgress struct {
	Phase            string        `json:"phase"`
	Protocol         string        `json:"protocol,omitempty"`
	TransferredBytes int64         `json:"transferred_bytes"`
	TransferBytes    int64         `json:"transfer_bytes"`
	Published        *UploadResult `json:"-"`
}
type UploadResult struct {
	Directory       string         `json:"directory,omitempty"`
	Files           []UploadedFile `json:"files,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
	RemoteArtifacts []string       `json:"remote_artifacts,omitempty"`
}
type UploadView struct {
	ID     string         `json:"upload_id"`
	Target ResolvedTarget `json:"target"`
	Status string         `json:"status"`
	UploadProgress
	UploadResult
	FilesReady        bool       `json:"files_ready"`
	RemoteMayContinue bool       `json:"remote_may_continue"`
	AcceptedAt        time.Time  `json:"accepted_at"`
	DeadlineAt        time.Time  `json:"deadline_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	Error             *Fault     `json:"error,omitempty"`
}

func uploadFilesReady(files []UploadedFile) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if file.Status != "completed" {
			return false
		}
	}
	return true
}

type upload struct {
	view   UploadView
	record *requestRecord
	cancel context.CancelFunc
	done   chan struct{}
}

func normalizeUpload(in UploadInput) (UploadInput, int, error) {
	wait := 5000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	in.WaitMS, in.RequestKey = nil, ""
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 1800000
	}
	if in.Source != "local" {
		return in, wait, fault("LOCAL_SOURCE_REQUIRED", "本阶段仅支持 source=local，源路径必须明确属于 MCP 服务电脑；HTTP 中转尚未提供")
	}
	if wait < 0 || wait > 5000 || in.TimeoutMS < 1 || in.TimeoutMS > 3600000 || len(in.LocalPaths) == 0 || len(in.LocalPaths) > 100 {
		return in, wait, fault("INVALID_ARGUMENT", "上传需提供 1–100 个本机绝对路径，等待不超过 5 秒，任务时限不超过 1 小时")
	}
	if (in.ConnectionID == "") == (in.Target.Profile == "") || (in.ConnectionID != "" && in.Target != (Target{})) {
		return in, wait, fault("INVALID_ARGUMENT", "请提供明确目标或 connection_id，不能同时提供")
	}
	if len(in.Target.Profile)+len(in.Target.Organization)+len(in.Target.AssetID)+len(in.Target.AccountID)+len(in.Target.Alias)+len(in.ConnectionID) > 4096 {
		return in, wait, fault("INVALID_ARGUMENT", "上传目标标识过长")
	}
	if !path.IsAbs(in.RemoteDirectory) || strings.ContainsRune(in.RemoteDirectory, 0) || !utf8.ValidString(in.RemoteDirectory) || len(in.RemoteDirectory) > 4096 {
		return in, wait, fault("INVALID_ARGUMENT", "远端目标目录须为绝对 UTF-8 路径")
	}
	in.RemoteDirectory = path.Clean(in.RemoteDirectory)
	in.LocalPaths = slices.Clone(in.LocalPaths)
	for i, p := range in.LocalPaths {
		if !filepath.IsAbs(p) || strings.ContainsRune(p, 0) || !utf8.ValidString(p) || len(p) > 4096 {
			return in, wait, fault("INVALID_ARGUMENT", "上传源须为 MCP 服务电脑上的绝对 UTF-8 路径")
		}
		in.LocalPaths[i] = filepath.Clean(p)
	}
	sort.Strings(in.LocalPaths)
	in.LocalPaths = slices.Compact(in.LocalPaths)
	return in, wait, nil
}
func (e *Engine) UploadFiles(ctx context.Context, in UploadInput) (UploadView, error) {
	normalized, wait, err := normalizeUpload(in)
	if err != nil {
		return UploadView{}, err
	}
	if err = ctx.Err(); err != nil {
		return UploadView{}, err
	}
	e.mu.Lock()
	var job *upload
	var bound *ResolvedTarget
	rec, fresh, err := e.requests.begin("upload", in.RequestKey, normalized, func() (string, error) {
		if e.closing {
			return "", fault("SERVICE_DRAINING", "服务正在停止")
		}
		active := 0
		for _, j := range e.uploads {
			if !terminal(j.view.Status) {
				active++
			}
		}
		if active >= 4 {
			return "", fault("LIMIT_EXCEEDED", "最多同时执行 4 个上传任务")
		}
		if in.ConnectionID != "" {
			c, err := e.findConnection(in.ConnectionID)
			if err != nil {
				return "", err
			}
			if c.closing || c.view.Status == "closed" {
				return "", fault("CONNECTION_CLOSED", "连接已关闭")
			}
			v := c.view.Target
			bound = &v
		}
		// 每项任务为 SSH 和 SFTP 各预留一个名额，避免压缩后才发现无法建连。
		for e.transportCount()+2 > e.opts.Limits.Transports {
			victim := e.idleLRU(nil, true)
			if victim == nil {
				return "", fault("LIMIT_EXCEEDED", "上传所需的独立连接名额不足")
			}
			e.dropLocked(victim, "upload_capacity")
		}
		id := e.nextID("u")
		// 为活动任务的路径、清单和返回结果预留空间，退休实例也共用宿主上限。
		for e.history.uploadBytes+(16<<20) > 64<<20 {
			var victim *operationRecord
			for n := e.history.fifo.Front(); n != nil; n = n.Next() {
				r := n.Value.(*operationRecord)
				if !r.active && r.uploadBytes > 0 {
					victim = r
					break
				}
			}
			if victim == nil {
				return "", fault("LIMIT_EXCEEDED", "上传元数据已达到宿主内存上限")
			}
			e.history.removeLocked(victim)
		}
		op := e.history.addLocked(id, true, e)
		if op == nil {
			return "", fault("LIMIT_EXCEEDED", "操作记录全部活跃")
		}
		now := e.opts.Now()
		job = &upload{view: UploadView{ID: id, Status: "accepted", UploadProgress: UploadProgress{Phase: "preparing"}, AcceptedAt: now, DeadlineAt: now.Add(time.Duration(normalized.TimeoutMS) * time.Millisecond)}, done: make(chan struct{})}
		if bound != nil {
			job.view.Target = *bound
		}
		e.uploads[id] = job
		op.upload = job
		op.uploadBytes = 16 << 20
		e.history.uploadBytes += op.uploadBytes
		e.uploadTransports += 2
		e.uploadEventLocked(job, "upload_accepted", "上传任务已受理")
		return id, nil
	})
	if err == nil {
		if fresh {
			job.record = rec
			jobCtx, cancel := context.WithTimeout(e.ctx, time.Duration(normalized.TimeoutMS)*time.Millisecond)
			job.cancel = cancel
			go e.runUpload(jobCtx, job, normalized, bound)
		} else {
			job = e.uploads[rec.ResourceID]
		}
	}
	e.mu.Unlock()
	if err != nil {
		return UploadView{}, err
	}
	if job == nil {
		return UploadView{}, fault("UPLOAD_EXPIRED", "上传详细结果已回收，不会重新上传")
	}
	if !fresh {
		e.RecordCall("upload_files", nil, CallDetails{Summary: "重复请求，返回原上传任务  upload_id=" + job.view.ID})
	}
	return e.waitUpload(ctx, job, wait)
}
func (e *Engine) runUpload(ctx context.Context, job *upload, in UploadInput, bound *ResolvedTarget) {
	defer job.cancel()
	var target ResolvedTarget
	var err error
	if bound != nil {
		target = *bound
	} else if e.opts.Resolve != nil {
		target, err = e.opts.Resolve(ctx, in.Target)
	} else {
		err = fault("TARGET_UNAVAILABLE", "目标解析不可用")
	}
	if err == nil && e.opts.Validate != nil {
		err = e.opts.Validate(ctx, target)
	}
	e.mu.Lock()
	job.view.Target = target
	if job.view.Status == "accepted" {
		job.view.Status = "running"
	}
	e.mu.Unlock()
	var result UploadResult
	if err == nil {
		runner := e.opts.Upload
		if runner == nil {
			runner = e.performUpload
		}
		result, err = runner(ctx, target, in, func(p UploadProgress) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if terminal(job.view.Status) {
				return
			}
			if p.Published != nil {
				v := cloneUpload(UploadView{UploadResult: *p.Published})
				job.view.UploadResult = v.UploadResult
				p.Published = nil
			}
			changed := job.view.Phase != p.Phase || job.view.Protocol != p.Protocol
			job.view.UploadProgress = p
			if changed {
				e.uploadEventLocked(job, "upload_progress", p.Phase+"  protocol="+p.Protocol)
			}
		})
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if terminal(job.view.Status) {
		return
	}
	job.view.UploadResult = result
	status := "completed"
	if err != nil {
		job.view.Error = asFault(err, "UPLOAD_FAILED", "上传失败，请检查本机源文件、网络和远端目标目录")
		status = "failed"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
			job.view.Error = fault("CANCELLED", "上传已取消")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			status = "timed_out"
			job.view.Error = fault("TIMED_OUT", "上传超过任务时限")
		}
		if job.view.Error.Code == "REMOTE_STATE_UNKNOWN" || job.view.Error.Code == "UPLOAD_RESULT_UNKNOWN" {
			status = "unknown"
			job.view.RemoteMayContinue = job.view.Error.Code == "REMOTE_STATE_UNKNOWN"
		}
	}
	e.finishUploadLocked(job, status)
}
func (e *Engine) finishUploadLocked(job *upload, status string) {
	job.view.Status = status
	job.view.FilesReady = status == "completed" && uploadFilesReady(job.view.Files)
	if status == "completed" {
		job.view.Phase = "completed"
	}
	now := e.opts.Now()
	job.view.FinishedAt = &now
	message := status + "  directory=" + job.view.Directory
	if job.view.Error != nil {
		message += "  error=" + job.view.Error.Code + "  " + job.view.Error.Message
	}
	e.uploadEventLocked(job, "upload_finished", message)
	for _, warning := range job.view.Warnings {
		e.uploadEventLocked(job, "upload_warning", warning)
	}
	if r := e.history.records[job.view.ID]; r != nil {
		r.active = false
		payload, _ := json.Marshal(job.view)
		size := len(payload)
		e.history.uploadBytes += size - r.uploadBytes
		r.uploadBytes = size
	}
	e.uploadTransports -= 2
	e.requests.finish(job.record, job.view.Error)
	close(job.done)
}
func (e *Engine) uploadEventLocked(job *upload, event, text string) {
	severity := "INFO"
	detail := 2
	if event == "upload_warning" {
		severity = "WARN"
		detail = 1
	}
	if event == "upload_finished" && job.view.Error != nil {
		severity = "ERROR"
		detail = 1
	}
	e.history.eventLocked(e.history.records[job.view.ID], LogLine{Instance: e.opts.Instance, Event: event, Severity: severity, Detail: detail, Text: job.view.ID + "  " + text}, false)
}
func cloneUpload(v UploadView) UploadView {
	v.Files = slices.Clone(v.Files)
	v.Warnings = slices.Clone(v.Warnings)
	v.RemoteArtifacts = slices.Clone(v.RemoteArtifacts)
	if v.Error != nil {
		x := *v.Error
		v.Error = &x
	}
	return v
}
func (e *Engine) waitUpload(ctx context.Context, job *upload, ms int) (UploadView, error) {
	if ms > 0 {
		timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-job.done:
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.uploads[job.view.ID] != job {
		return UploadView{}, fault("UPLOAD_EXPIRED", "等待期间上传记录已回收")
	}
	return cloneUpload(job.view), nil
}
func (e *Engine) findUpload(id string) (*upload, error) {
	if err := e.checkID(id); err != nil {
		return nil, err
	}
	job := e.uploads[id]
	if job == nil {
		return nil, fault("UPLOAD_EXPIRED", "上传记录不存在或已过期；不能据此自动重试")
	}
	return job, nil
}
func (e *Engine) GetUpload(ctx context.Context, in GetUploadInput) (UploadView, error) {
	wait := 20000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	if wait < 0 || wait > 30000 {
		return UploadView{}, fault("INVALID_ARGUMENT", "查询等待窗口须为 0–30000ms")
	}
	e.mu.Lock()
	job, err := e.findUpload(in.UploadID)
	e.mu.Unlock()
	if err != nil {
		return UploadView{}, err
	}
	return e.waitUpload(ctx, job, wait)
}
func (e *Engine) CancelUpload(_ context.Context, in UploadIDInput) (UploadView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job, err := e.findUpload(in.UploadID)
	if err != nil {
		return UploadView{}, err
	}
	if !terminal(job.view.Status) {
		job.view.Status = "cancelling"
		job.cancel()
	}
	return cloneUpload(job.view), nil
}
func (e *Engine) UploadsPage(_ context.Context, in UploadsPageInput) (Page[UploadView], error) {
	if in.Status != "" && !terminal(in.Status) && in.Status != "accepted" && in.Status != "running" && in.Status != "cancelling" {
		return Page[UploadView]{}, fault("INVALID_ARGUMENT", "上传状态无效")
	}
	offset, limit, key, err := e.pages.page(in.PageInput, "uploads", in.Status)
	if err != nil {
		return Page[UploadView]{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	all := []UploadView{}
	for _, job := range e.uploads {
		if in.Status == "" || job.view.Status == in.Status {
			v := job.view
			v.Files = nil
			v = cloneUpload(v)
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return pageResult(e.pages, window(all, offset, limit), len(all), offset, key, "", ""), nil
}
func (e *Engine) pruneUploadsLocked() {
	for _, job := range e.uploads {
		if job.view.FinishedAt != nil && e.opts.Now().Sub(*job.view.FinishedAt) >= e.opts.Limits.HistoryTTL {
			if r := e.history.records[job.view.ID]; r != nil {
				e.history.removeLocked(r)
			}
		}
	}
}
