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

	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
)

type DownloadInput struct {
	Target         Target   `json:"target,omitempty"`
	ConnectionID   string   `json:"connection_id,omitempty"`
	RequestKey     string   `json:"request_key"`
	RemotePaths    []string `json:"remote_paths"`
	LocalDirectory string   `json:"local_directory,omitempty"`
	TimeoutMS      int      `json:"timeout_ms,omitempty"`
	WaitMS         *int     `json:"wait_ms,omitempty"`
}
type DownloadIDInput struct {
	DownloadID string `json:"download_id"`
}
type GetDownloadInput struct {
	DownloadID string `json:"download_id"`
	WaitMS     *int   `json:"wait_ms,omitempty"`
}
type DownloadsPageInput struct {
	PageInput
	Status string `json:"status,omitempty"`
}
type DownloadedFile struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Size   int64  `json:"size"`
	// 保留可选字段以兼容 MCP schema；自动下载不再计算或填充内容哈希。
	SHA256     string `json:"sha256,omitempty"`
	CapturedAt string `json:"captured_at,omitempty"`
}
type DownloadProgress struct {
	Phase            string          `json:"phase"`
	Protocol         string          `json:"protocol,omitempty"`
	TransferredBytes int64           `json:"transferred_bytes"`
	TransferBytes    int64           `json:"transfer_bytes"`
	Published        *DownloadResult `json:"-"`
}
type DownloadResult struct {
	Delivery        string           `json:"delivery,omitempty"`
	Link            *DownloadLink    `json:"link,omitempty"`
	Instructions    string           `json:"instructions,omitempty"`
	RenamedEntries  []DownloadRename `json:"renamed_entries,omitempty"`
	Directory       string           `json:"directory,omitempty"`
	Files           []DownloadedFile `json:"files,omitempty"`
	Warnings        []string         `json:"warnings,omitempty"`
	RemoteArtifacts []string         `json:"remote_artifacts,omitempty"`
}

// ArchivePath 是准备包中的相对位置；只有同名独立来源被改名时返回对应关系。
type DownloadRename struct {
	Source      string `json:"source"`
	ArchivePath string `json:"archive_path"`
}
type DownloadLink struct {
	URL                string                 `json:"url,omitempty"`
	FileName           string                 `json:"file_name"`
	Format             string                 `json:"format"`
	Size               int64                  `json:"size"`
	ExpiresAt          time.Time              `json:"expires_at"`
	RequiresExtraction bool                   `json:"requires_extraction"`
	RestoreName        string                 `json:"restore_name"`
	Artifact           downloadcache.Artifact `json:"-"`
	Available          bool                   `json:"available"`
	UnavailableReason  string                 `json:"unavailable_reason,omitempty"`
}
type DownloadView struct {
	ID     string         `json:"download_id"`
	Target ResolvedTarget `json:"target"`
	Status string         `json:"status"`
	DownloadProgress
	DownloadResult
	FilesReady        bool       `json:"files_ready"`
	RemoteMayContinue bool       `json:"remote_may_continue"`
	AcceptedAt        time.Time  `json:"accepted_at"`
	DeadlineAt        time.Time  `json:"deadline_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	Error             *Fault     `json:"error,omitempty"`
}
type download struct {
	view   DownloadView
	record *requestRecord
	cancel context.CancelFunc
	done   chan struct{}
}

func normalizeDownload(in DownloadInput) (DownloadInput, int, error) {
	return normalizeDownloadForMode(in, "local")
}

func normalizeDownloadForMode(in DownloadInput, mode string) (DownloadInput, int, error) {
	wait := 5000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	in.WaitMS = nil
	in.RequestKey = ""
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 1800000
	}
	if wait < 0 || wait > 5000 || in.TimeoutMS < 1 || in.TimeoutMS > 3600000 || len(in.RemotePaths) == 0 || len(in.RemotePaths) > 100 {
		return in, wait, fault("INVALID_ARGUMENT", "下载需提供 1–100 个绝对路径，等待不超过 5 秒，任务时限不超过 1 小时")
	}
	if (in.ConnectionID == "") == (in.Target.Profile == "") || (in.ConnectionID != "" && in.Target != (Target{})) {
		return in, wait, fault("INVALID_ARGUMENT", "请提供明确目标或 connection_id，不能同时提供")
	}
	if len(in.Target.Profile)+len(in.Target.Organization)+len(in.Target.AssetID)+len(in.Target.AccountID)+len(in.Target.Alias)+len(in.ConnectionID) > 4096 {
		return in, wait, fault("INVALID_ARGUMENT", "下载目标标识过长")
	}
	in.RemotePaths = slices.Clone(in.RemotePaths)
	for i, p := range in.RemotePaths {
		if !path.IsAbs(p) || strings.ContainsRune(p, 0) || !utf8.ValidString(p) || len(p) > 4096 {
			return in, wait, fault("INVALID_ARGUMENT", "远端路径须为绝对 UTF-8 路径，不能包含 NUL")
		}
		in.RemotePaths[i] = path.Clean(p)
	}
	sort.Strings(in.RemotePaths)
	in.RemotePaths = slices.Compact(in.RemotePaths)
	if in.LocalDirectory != "" {
		if (mode != "link" && !filepath.IsAbs(in.LocalDirectory)) || strings.ContainsRune(in.LocalDirectory, 0) || !utf8.ValidString(in.LocalDirectory) || len(in.LocalDirectory) > 4096 {
			return in, wait, fault("INVALID_ARGUMENT", "本地保存目录须为绝对路径")
		}
		if mode != "link" {
			in.LocalDirectory = filepath.Clean(in.LocalDirectory)
		}
	}
	return in, wait, nil
}

func (e *Engine) DownloadFiles(ctx context.Context, in DownloadInput) (DownloadView, error) {
	normalized, wait, err := normalizeDownloadForMode(in, e.opts.DeliveryMode)
	if err != nil {
		return DownloadView{}, err
	}
	if err = ctx.Err(); err != nil {
		return DownloadView{}, err
	}
	e.mu.Lock()
	var job *download
	var bound *ResolvedTarget
	rec, fresh, err := e.requests.begin("download", in.RequestKey, normalized, func() (string, error) {
		if e.closing {
			return "", fault("SERVICE_DRAINING", "服务正在停止")
		}
		active := 0
		for _, j := range e.downloads {
			if !terminal(j.view.Status) {
				active++
			}
		}
		if active >= 4 {
			return "", fault("LIMIT_EXCEEDED", "最多同时执行 4 个下载任务")
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
				return "", fault("LIMIT_EXCEEDED", "下载所需的独立连接名额不足")
			}
			e.dropLocked(victim, "download_capacity")
		}
		id := e.nextID("d")
		// 为活动任务的路径、清单和返回结果预留空间，退休实例也共用宿主上限。
		for e.history.downloadBytes+(16<<20) > 64<<20 {
			var victim *operationRecord
			for n := e.history.fifo.Front(); n != nil; n = n.Next() {
				r := n.Value.(*operationRecord)
				if !r.active && r.downloadBytes > 0 {
					victim = r
					break
				}
			}
			if victim == nil {
				return "", fault("LIMIT_EXCEEDED", "下载元数据已达到宿主内存上限")
			}
			e.history.removeLocked(victim)
		}
		op := e.history.addLocked(id, true, e)
		if op == nil {
			return "", fault("LIMIT_EXCEEDED", "操作记录全部活跃")
		}
		now := e.opts.Now()
		job = &download{view: DownloadView{ID: id, Status: "accepted", DownloadProgress: DownloadProgress{Phase: "preparing"}, AcceptedAt: now, DeadlineAt: now.Add(time.Duration(normalized.TimeoutMS) * time.Millisecond)}, done: make(chan struct{})}
		if bound != nil {
			job.view.Target = *bound
		}
		e.downloads[id] = job
		op.download = job
		op.downloadBytes = 16 << 20
		e.history.downloadBytes += op.downloadBytes
		e.downloadTransports += 2
		e.downloadEventLocked(job, "download_accepted", "下载任务已受理")
		return id, nil
	})
	if err == nil {
		if fresh {
			job.record = rec
			jobCtx, cancel := context.WithTimeout(e.ctx, time.Duration(normalized.TimeoutMS)*time.Millisecond)
			job.cancel = cancel
			go e.runDownload(jobCtx, job, normalized, bound)
		} else {
			job = e.downloads[rec.ResourceID]
		}
	}
	e.mu.Unlock()
	if err != nil {
		return DownloadView{}, err
	}
	if job == nil {
		return DownloadView{}, fault("DOWNLOAD_EXPIRED", "下载详细结果已回收，不会重新下载")
	}
	if !fresh {
		e.RecordCall("download_files", nil, CallDetails{Summary: "重复请求，返回原下载任务  download_id=" + job.view.ID})
	}
	return e.waitDownload(ctx, job, wait)
}
func (e *Engine) runDownload(ctx context.Context, job *download, in DownloadInput, bound *ResolvedTarget) {
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
	var result DownloadResult
	if err == nil {
		runner := e.opts.Download
		if runner == nil {
			runner = e.performDownload
		}
		result, err = runner(ctx, target, in, func(p DownloadProgress) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if terminal(job.view.Status) {
				return
			}
			if p.Published != nil {
				v := cloneDownload(DownloadView{DownloadResult: *p.Published})
				job.view.DownloadResult = v.DownloadResult
				p.Published = nil
			}
			changed := job.view.Phase != p.Phase || job.view.Protocol != p.Protocol
			job.view.DownloadProgress = p
			if changed {
				e.downloadEventLocked(job, "download_progress", p.Phase+"  protocol="+p.Protocol)
			}
		})
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if terminal(job.view.Status) {
		return
	}
	job.view.DownloadResult = result
	status := "completed"
	if err != nil {
		job.view.Error = asFault(err, "DOWNLOAD_FAILED", "下载失败，请检查目标文件、网络和本地保存目录")
		status = "failed"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
			job.view.Error = fault("CANCELLED", "下载已取消")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			status = "timed_out"
			job.view.Error = fault("TIMED_OUT", "下载超过任务时限")
		}
		if job.view.Error.Code == "REMOTE_STATE_UNKNOWN" {
			status = "unknown"
			job.view.RemoteMayContinue = true
		}
	}
	e.finishDownloadLocked(job, status)
}
func (e *Engine) finishDownloadLocked(job *download, status string) {
	job.view.Status = status
	job.view.FilesReady = status == "completed" && job.view.Delivery != "link"
	if status == "completed" {
		job.view.Phase = "completed"
	}
	now := e.opts.Now()
	job.view.FinishedAt = &now
	message := status + "  directory=" + job.view.Directory
	if job.view.Error != nil {
		message += "  error=" + job.view.Error.Code + "  " + job.view.Error.Message
	}
	e.downloadEventLocked(job, "download_finished", message)
	for _, warning := range job.view.Warnings {
		e.downloadEventLocked(job, "download_warning", warning)
	}
	if r := e.history.records[job.view.ID]; r != nil {
		r.active = false
		payload, _ := json.Marshal(job.view)
		size := len(payload)
		e.history.downloadBytes += size - r.downloadBytes
		r.downloadBytes = size
	}
	e.downloadTransports -= 2
	e.requests.finish(job.record, job.view.Error)
	close(job.done)
}
func (e *Engine) downloadEventLocked(job *download, event, text string) {
	severity := "INFO"
	detail := 2
	if event == "download_warning" {
		severity = "WARN"
		detail = 1
	}
	if event == "download_finished" && job.view.Error != nil {
		severity = "ERROR"
		detail = 1
	}
	e.history.eventLocked(e.history.records[job.view.ID], LogLine{Instance: e.opts.Instance, Event: event, Severity: severity, Detail: detail, Text: job.view.ID + "  " + text}, false)
}
func cloneDownload(v DownloadView) DownloadView {
	v.Files = slices.Clone(v.Files)
	v.Warnings = slices.Clone(v.Warnings)
	v.RemoteArtifacts = slices.Clone(v.RemoteArtifacts)
	v.RenamedEntries = slices.Clone(v.RenamedEntries)
	if v.Link != nil {
		x := *v.Link
		v.Link = &x
	}
	if v.Error != nil {
		x := *v.Error
		v.Error = &x
	}
	return v
}
func (e *Engine) waitDownload(ctx context.Context, job *download, ms int) (DownloadView, error) {
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
	if e.downloads[job.view.ID] != job {
		return DownloadView{}, fault("DOWNLOAD_EXPIRED", "等待期间下载记录已回收")
	}
	return cloneDownload(job.view), nil
}
func (e *Engine) findDownload(id string) (*download, error) {
	if err := e.checkID(id); err != nil {
		return nil, err
	}
	job := e.downloads[id]
	if job == nil {
		return nil, fault("DOWNLOAD_EXPIRED", "下载记录不存在或已过期；不能据此自动重试")
	}
	return job, nil
}
func (e *Engine) GetDownload(ctx context.Context, in GetDownloadInput) (DownloadView, error) {
	wait := 20000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	if wait < 0 || wait > 30000 {
		return DownloadView{}, fault("INVALID_ARGUMENT", "查询等待窗口须为 0–30000ms")
	}
	e.mu.Lock()
	job, err := e.findDownload(in.DownloadID)
	e.mu.Unlock()
	if err != nil {
		return DownloadView{}, err
	}
	return e.waitDownload(ctx, job, wait)
}
func (e *Engine) CancelDownload(_ context.Context, in DownloadIDInput) (DownloadView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job, err := e.findDownload(in.DownloadID)
	if err != nil {
		return DownloadView{}, err
	}
	if !terminal(job.view.Status) {
		job.view.Status = "cancelling"
		job.cancel()
	}
	return cloneDownload(job.view), nil
}
func (e *Engine) DownloadsPage(_ context.Context, in DownloadsPageInput) (Page[DownloadView], error) {
	if in.Status != "" && !terminal(in.Status) && in.Status != "accepted" && in.Status != "running" && in.Status != "cancelling" {
		return Page[DownloadView]{}, fault("INVALID_ARGUMENT", "下载状态无效")
	}
	offset, limit, key, err := e.pages.page(in.PageInput, "downloads", in.Status)
	if err != nil {
		return Page[DownloadView]{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	all := []DownloadView{}
	for _, job := range e.downloads {
		if in.Status == "" || job.view.Status == in.Status {
			v := job.view
			v.Files = nil
			v = cloneDownload(v)
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return pageResult(e.pages, window(all, offset, limit), len(all), offset, key, "", ""), nil
}
func (e *Engine) pruneDownloadsLocked() {
	for _, job := range e.downloads {
		if job.view.FinishedAt != nil && e.opts.Now().Sub(*job.view.FinishedAt) >= e.opts.Limits.HistoryTTL {
			if r := e.history.records[job.view.ID]; r != nil {
				e.history.removeLocked(r)
			}
		}
	}
}
