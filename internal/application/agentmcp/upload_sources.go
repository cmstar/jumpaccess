package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
)

type UploadEntry struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Directory bool   `json:"directory"`
}
type PrepareUploadInput struct {
	Target          Target        `json:"target,omitempty"`
	ConnectionID    string        `json:"connection_id,omitempty"`
	RequestKey      string        `json:"request_key"`
	RemoteDirectory string        `json:"remote_directory"`
	Format          string        `json:"format"`
	Size            int64         `json:"size"`
	Entries         []UploadEntry `json:"entries"`
}
type UploadSourceInput struct {
	SourceID string `json:"source_id"`
}
type CommitUploadInput struct {
	SourceID   string `json:"source_id"`
	RequestKey string `json:"request_key"`
	TimeoutMS  int    `json:"timeout_ms,omitempty"`
	WaitMS     *int   `json:"wait_ms,omitempty"`
}
type UploadSourceView struct {
	uploadcache.View
	Target          ResolvedTarget `json:"target"`
	RemoteDirectory string         `json:"remote_directory,omitempty"`
	Format          string         `json:"format,omitempty"`
	EntryCount      int            `json:"entry_count"`
	URL             string         `json:"url,omitempty"`
	Method          string         `json:"method,omitempty"`
}
type uploadSourceMetadata struct {
	Target                  ResolvedTarget
	RemoteDirectory, Format string
	Entries                 []UploadEntry
}

func normalizeUploadSource(in PrepareUploadInput) (PrepareUploadInput, error) {
	in.RequestKey = ""
	if (in.ConnectionID == "") == (in.Target == (Target{})) {
		return in, fault("INVALID_ARGUMENT", "请提供明确目标或 connection_id，不能同时提供")
	}
	if len(in.Target.Profile)+len(in.Target.Name)+len(in.Target.Organization)+len(in.Target.AssetID)+len(in.Target.AccountID)+len(in.Target.Alias)+len(in.ConnectionID) > 4096 {
		return in, fault("INVALID_ARGUMENT", "上传目标标识过长")
	}
	if !path.IsAbs(in.RemoteDirectory) || !utf8.ValidString(in.RemoteDirectory) || len(in.RemoteDirectory) > 4096 || strings.ContainsRune(in.RemoteDirectory, 0) {
		return in, fault("INVALID_ARGUMENT", "远端目标目录须为绝对 UTF-8 路径")
	}
	in.RemoteDirectory = path.Clean(in.RemoteDirectory)
	if in.Size < 0 || in.Size > 1<<30 || len(in.Entries) == 0 || len(in.Entries) > 1000 {
		return in, fault("INVALID_MANIFEST", "传输包最多 1 GiB，清单需包含 1–1000 项")
	}
	if in.Format != "raw" && in.Format != "gzip" && in.Format != "tar.gz" {
		return in, fault("INVALID_MANIFEST", "format 须为 raw、gzip 或 tar.gz，仅声明本次新增的压缩层")
	}
	in.Entries = append([]UploadEntry(nil), in.Entries...)
	seen := map[string]UploadEntry{}
	roots := 0
	for _, entry := range in.Entries {
		if !validDownloadName(entry.Name) || entry.Size < 0 || entry.Size > 1<<53 || (entry.Directory && entry.Size != 0) {
			return in, fault("INVALID_MANIFEST", "上传清单包含不支持的名称、类型或大小")
		}
		key := strings.ToLower(entry.Name)
		if _, ok := seen[key]; ok {
			return in, fault("UPLOAD_NAME_CONFLICT", "上传清单存在同名或大小写冲突")
		}
		parent := path.Dir(entry.Name)
		if parent != "." {
			p, ok := seen[strings.ToLower(parent)]
			if !ok || !p.Directory || p.Name != parent {
				return in, fault("INVALID_MANIFEST", "父目录必须先于子项列入清单")
			}
		} else {
			roots++
		}
		seen[key] = entry
	}
	if roots > 100 {
		return in, fault("INVALID_MANIFEST", "最多 100 个独立来源")
	}
	if in.Format != "tar.gz" && (len(in.Entries) != 1 || in.Entries[0].Directory || strings.Contains(in.Entries[0].Name, "/")) {
		return in, fault("INVALID_MANIFEST", "raw 和 gzip 只接收一个普通文件")
	}
	if in.Format == "raw" && in.Entries[0].Size != in.Size {
		return in, fault("INVALID_MANIFEST", "raw 文件长度须与准备包一致")
	}
	data, _ := json.Marshal(in)
	if len(data) > uploadcache.MaxMetadata-8192 {
		return in, fault("INVALID_MANIFEST", "上传清单超过元数据限制")
	}
	return in, nil
}

func (e *Engine) PrepareUpload(ctx context.Context, in PrepareUploadInput) (UploadSourceView, error) {
	normalized, err := normalizeUploadSource(in)
	if err != nil {
		return UploadSourceView{}, err
	}
	if err = ctx.Err(); err != nil {
		return UploadSourceView{}, err
	}
	e.mu.Lock()
	var bound *ResolvedTarget
	rec, fresh, err := e.requests.begin("prepare_upload", in.RequestKey, normalized, func() (string, error) {
		if e.closing {
			return "", fault("SERVICE_DRAINING", "服务正在停止")
		}
		if e.opts.UploadCache == nil {
			return "", fault("UPLOAD_SOURCE_UNAVAILABLE", "HTTP 上传接收未配置")
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
		source, err := e.opts.UploadCache.Reserve(normalized.Size, nil)
		if err != nil {
			return "", uploadSourceFault(err)
		}
		return source.ID, nil
	})
	e.mu.Unlock()
	if err != nil {
		return UploadSourceView{}, err
	}
	if fresh {
		go func() {
			prepCtx, cancel := context.WithTimeout(e.ctx, e.opts.Limits.ConnectTimeout)
			defer cancel()
			var target ResolvedTarget
			var prepareErr error
			if bound != nil {
				target = *bound
			} else if e.opts.Resolve != nil {
				target, prepareErr = e.opts.Resolve(prepCtx, normalized.Target)
			} else {
				prepareErr = fault("TARGET_UNAVAILABLE", "目标解析不可用")
			}
			if prepareErr == nil && e.opts.Validate != nil {
				prepareErr = e.opts.Validate(prepCtx, target)
			}
			if prepareErr == nil {
				metadata, _ := json.Marshal(uploadSourceMetadata{Target: target, RemoteDirectory: normalized.RemoteDirectory, Format: normalized.Format, Entries: normalized.Entries})
				prepareErr = e.opts.UploadCache.SetMetadata(rec.ResourceID, metadata)
			}
			var failure *Fault
			if prepareErr != nil {
				_, _ = e.opts.UploadCache.Cancel(rec.ResourceID)
				failure = asFault(prepareErr, "UPLOAD_SOURCE_PREPARATION_FAILED", "无法准备上传接收，请检查目标身份与服务状态")
			}
			e.requests.finish(rec, failure)
		}()
	}
	select {
	case <-rec.done:
		if rec.Failure != nil {
			return UploadSourceView{}, rec.Failure
		}
	case <-ctx.Done():
	}
	return e.GetUploadSource(ctx, UploadSourceInput{SourceID: rec.ResourceID})
}
func (e *Engine) GetUploadSource(_ context.Context, in UploadSourceInput) (UploadSourceView, error) {
	if e.opts.UploadCache == nil {
		return UploadSourceView{}, fault("UPLOAD_SOURCE_UNAVAILABLE", "HTTP 上传接收未配置")
	}
	v, err := e.opts.UploadCache.Get(in.SourceID)
	if err != nil {
		return UploadSourceView{}, uploadSourceFault(err)
	}
	out := UploadSourceView{View: v}
	if len(v.Metadata) == 0 {
		out.State = "preparing"
		return out, nil
	}
	var meta uploadSourceMetadata
	if json.Unmarshal(v.Metadata, &meta) != nil {
		return UploadSourceView{}, fault("INVALID_MANIFEST", "上传接收元数据无效")
	}
	out.Target = meta.Target
	out.RemoteDirectory = meta.RemoteDirectory
	out.Format = meta.Format
	out.EntryCount = len(meta.Entries)
	out.Metadata = nil
	return out, nil
}
func (e *Engine) CancelUploadSource(_ context.Context, in UploadSourceInput) (UploadSourceView, error) {
	if e.opts.UploadCache == nil {
		return UploadSourceView{}, fault("UPLOAD_SOURCE_UNAVAILABLE", "HTTP 上传接收未配置")
	}
	v, err := e.opts.UploadCache.Cancel(in.SourceID)
	if err != nil {
		return UploadSourceView{}, uploadSourceFault(err)
	}
	v.Metadata = nil
	return UploadSourceView{View: v}, nil
}
func (e *Engine) CommitUpload(ctx context.Context, in CommitUploadInput) (UploadView, error) {
	wait := 5000
	if in.WaitMS != nil {
		wait = *in.WaitMS
	}
	in.WaitMS = nil
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 1800000
	}
	if in.SourceID == "" || len(in.SourceID) > 128 || wait < 0 || wait > 5000 || in.TimeoutMS < 1 || in.TimeoutMS > 3600000 {
		return UploadView{}, fault("INVALID_ARGUMENT", "需接收对象 ID，等待不超过 5 秒，任务时限不超过 1 小时")
	}
	key := in.RequestKey
	in.RequestKey = ""
	return e.startUpload(ctx, UploadInput{Source: "http", TimeoutMS: in.TimeoutMS, sourceID: in.SourceID}, "commit_upload", key, in, wait)
}
func (e *Engine) claimUploadSource(id, task string) (*preparedUpload, uploadSourceMetadata, error) {
	var meta uploadSourceMetadata
	if e.opts.UploadCache == nil {
		return nil, meta, fault("UPLOAD_SOURCE_UNAVAILABLE", "HTTP 上传接收未配置")
	}
	source, err := e.opts.UploadCache.Get(id)
	if err != nil {
		return nil, meta, uploadSourceFault(err)
	}
	if len(source.Metadata) == 0 {
		return nil, meta, fault("UPLOAD_SOURCE_NOT_READY", "上传接收尚未准备完成")
	}
	if json.Unmarshal(source.Metadata, &meta) != nil {
		return nil, meta, fault("INVALID_MANIFEST", "上传接收元数据无效")
	}
	lease, err := e.opts.UploadCache.Claim(id, task)
	if err != nil {
		return nil, meta, uploadSourceFault(err)
	}
	p := &preparedUpload{File: lease.File, Format: meta.Format, Size: source.Size, release: lease.Close}
	for _, entry := range meta.Entries {
		p.Entries = append(p.Entries, remoteEntry{Name: entry.Name, Size: entry.Size, Directory: entry.Directory})
	}
	return p, meta, nil
}
func uploadSourceFault(err error) *Fault {
	code, message := "UPLOAD_SOURCE_FAILED", "上传接收暂不可用，请检查服务缓存"
	switch {
	case errors.Is(err, uploadcache.ErrExpired):
		code, message = "UPLOAD_SOURCE_EXPIRED", "上传接收对象已失效，不会重新接收或上传；如曾提交，先查询原任务"
	case errors.Is(err, uploadcache.ErrNotReady):
		code, message = "UPLOAD_SOURCE_NOT_READY", "内容尚未完整接收，不能提交远端上传"
	case errors.Is(err, uploadcache.ErrUsed):
		code, message = "UPLOAD_SOURCE_USED", "接收内容已被任务接管，不能创建另一项上传；请查询原任务"
	case errors.Is(err, uploadcache.ErrCapacity):
		code, message = "UPLOAD_CACHE_FULL", "上传缓存额度不足，请等待过期或取消不再使用的接收对象"
	case errors.Is(err, uploadcache.ErrClosed):
		code, message = "SERVICE_DRAINING", "HTTP 上传接收服务已停止"
	case errors.Is(err, uploadcache.ErrTooLarge):
		code, message = "TRANSFER_TOO_LARGE", "上传准备包或元数据超过上限"
	}
	return fault(code, message)
}
