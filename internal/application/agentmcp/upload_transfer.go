package agentmcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/zmodem"
)

type UploadRemote interface {
	OpenFile(string, int) (io.WriteCloser, error)
	Close() error
}
type UploadSFTP struct {
	Client  UploadRemote
	Root    *string
	Account string
}

func (e *Engine) performUpload(ctx context.Context, target ResolvedTarget, in UploadInput, update func(UploadProgress)) (result UploadResult, err error) {
	result.Directory = in.RemoteDirectory
	update(UploadProgress{Phase: "preparing"})
	p, err := prepareUpload(ctx, in.LocalPaths, 1<<30)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := p.Close(); closeErr != nil {
			result.Warnings = append(result.Warnings, "本机上传准备包关闭或清理失败，请检查服务电脑临时目录")
		}
	}()
	entries := make([]remoteEntry, len(p.Entries))
	for i, entry := range p.Entries {
		entries[i] = remoteEntry{Name: entry.Name, Size: entry.Size, Directory: entry.Directory}
		if !strings.Contains(entry.Name, "/") {
			result.Files = append(result.Files, UploadedFile{Name: entry.Name, Source: entry.Source, Path: path.Join(in.RemoteDirectory, entry.Name), Directory: entry.Directory, Size: entry.Size, Status: "not_published"})
		}
	}
	dial := e.opts.DialUpload
	if dial == nil {
		dial = e.opts.Dial
	}
	if dial == nil {
		return result, fault("UPLOAD_UNAVAILABLE", "SSH 上传连接未配置")
	}
	connectCtx, cancel := context.WithTimeout(ctx, e.opts.Limits.ConnectTimeout)
	client, err := dial(connectCtx, target)
	cancel()
	if err != nil {
		return result, err
	}
	defer client.Close()
	owned := []ownedRemoteFile{}
	keepRemote := false
	defer func() {
		if len(owned) == 0 {
			return
		}
		if !keepRemote {
			cleanCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			_, _, cleanErr := remoteUploadCall(cleanCtx, client, map[string]any{"op": "cleanup", "files": owned}, nil)
			stop()
			if cleanErr == nil {
				return
			}
			result.Warnings = append(result.Warnings, "远端上传临时内容清理未确认，请检查 remote_artifacts")
		} else {
			result.Warnings = append(result.Warnings, "远端进程结束未确认，保留必要临时内容")
		}
		for _, item := range owned {
			result.RemoteArtifacts = append(result.RemoteArtifacts, item.Path)
		}
	}()
	call := func(callCtx context.Context, params map[string]any, consume func(io.Reader, io.Writer) error) (json.RawMessage, error) {
		body, created, callErr := remoteUploadCall(callCtx, client, params, consume)
		owned = append(owned, created...)
		if remoteUnknown(callErr) {
			keepRemote = true
		}
		return body, callErr
	}
	metadata := func(op string) map[string]any {
		return map[string]any{"op": op, "destination": in.RemoteDirectory, "entries": entries, "format": p.Format, "size": p.Size}
	}
	body, err := call(ctx, metadata("check_target"), nil)
	if err != nil {
		return result, err
	}
	var destination struct {
		Identity []uint64 `json:"identity"`
	}
	if json.Unmarshal(body, &destination) != nil || len(destination.Identity) != 2 {
		return result, fault("REMOTE_METADATA_INVALID", "目标目录身份无效")
	}
	body, err = call(ctx, map[string]any{"op": "environment"}, nil)
	if err != nil {
		return result, err
	}
	var environment struct {
		Home string `json:"home"`
		RZ   bool   `json:"rz"`
	}
	if json.Unmarshal(body, &environment) != nil || !path.IsAbs(environment.Home) {
		return result, fault("REMOTE_ENVIRONMENT_UNAVAILABLE", "无法确定远端暂存位置")
	}
	create := func(base string) (ownedRemoteFile, error) {
		name := ".jumpaccess-upload-" + rand.Text()
		body, createErr := call(ctx, map[string]any{"op": "create_stage", "stage": base, "name": name}, nil)
		if createErr != nil {
			return ownedRemoteFile{}, createErr
		}
		var stage ownedRemoteFile
		if json.Unmarshal(body, &stage) != nil || stage.Path != path.Join(base, name) || len(stage.Identity) != 2 {
			return stage, fault("REMOTE_METADATA_INVALID", "上传暂存目录身份无效")
		}
		return stage, nil
	}
	var sftp UploadSFTP
	var stage ownedRemoteFile
	virtual := ""
	if e.opts.OpenUploadSFTP != nil {
		// SFTP 客户端绑定传入的 context，须覆盖整个传输；连接超时由连接器单独控制。
		sftp, err = e.opts.OpenUploadSFTP(ctx, target)
		if isDownloadIdentityError(err) || isUploadPermissionError(err) {
			return result, err
		}
		if err == nil && sftp.Client != nil {
			defer sftp.Client.Close()
			for _, candidate := range downloadStagingCandidates(sftp.Root, environment.Home, sftp.Account, downloadCapability{}) {
				created, createErr := create(candidate.physical)
				if createErr != nil {
					if keepRemote || ctx.Err() != nil {
						return result, createErr
					}
					continue
				}
				value := rand.Text()
				probeCtx, stop := context.WithTimeout(ctx, 20*time.Second)
				stopIO := context.AfterFunc(probeCtx, func() { sftp.Client.Close() })
				writer, probeErr := sftp.Client.OpenFile(path.Join(candidate.virtual, path.Base(created.Path), "probe"), os.O_WRONLY|os.O_CREATE|os.O_EXCL)
				if probeErr == nil {
					_, probeErr = io.WriteString(writer, value)
					probeErr = errors.Join(probeErr, writer.Close())
				}
				stopIO()
				stop()
				if probeErr == nil {
					_, probeErr = call(ctx, map[string]any{"op": "check_probe", "stage": created, "value": value}, nil)
				}
				if probeErr == nil {
					stage = created
					virtual = path.Join(candidate.virtual, path.Base(created.Path))
					break
				}
				if keepRemote || ctx.Err() != nil {
					return result, probeErr
				}
			}
		}
	}
	protocol := "sftp"
	if virtual == "" {
		protocol = "zmodem"
	}
	progress := UploadProgress{Phase: "transferring", Protocol: protocol, TransferBytes: p.Size}
	update(progress)
	if protocol == "sftp" {
		stopIO := context.AfterFunc(ctx, func() { sftp.Client.Close() })
		writer, writeErr := sftp.Client.OpenFile(path.Join(virtual, "payload"), os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		var sourceErr error
		if writeErr == nil {
			dest := &downloadWriter{file: writer, update: func(n int64) { progress.TransferredBytes = n; update(progress) }}
			source := &uploadReadTracker{Reader: downloadContextReader{ctx, io.NewSectionReader(p.File, 0, p.Size)}}
			_, writeErr = io.CopyN(dest, source, p.Size)
			sourceErr = source.err
			writeErr = errors.Join(writeErr, writer.Close())
		}
		stopIO()
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if sourceErr != nil {
			return result, fault("SOURCE_CHANGED", "读取上传内容失败，不切换协议")
		}
		if p.Format == "raw" {
			if err = p.checkSources(); err != nil {
				return result, err
			}
		}
		if writeErr != nil {
			// 关闭专用 SFTP 后改用新的隔离目录，旧半成品不参与还原。
			if closeErr := sftp.Client.Close(); closeErr != nil {
				return result, fault("REMOTE_STATE_UNKNOWN", "SFTP 关闭未确认，未继续上传")
			}
			protocol = "zmodem"
			stage = ownedRemoteFile{}
			progress.Protocol = protocol
			progress.TransferredBytes = 0
			result.Warnings = append(result.Warnings, "SFTP 传输失败，改用 ZMODEM 传输同一份内容")
		}
	}
	if protocol == "zmodem" {
		if !environment.RZ {
			return result, fault("NO_UPLOAD_PROTOCOL", "SFTP 不可用，且远端未提供 rz")
		}
		if stage.Path == "" || virtual == "" {
			for _, base := range []string{environment.Home, "/tmp"} {
				stage, err = create(base)
				if err == nil {
					break
				}
				if keepRemote || ctx.Err() != nil {
					return result, err
				}
			}
			if err != nil {
				return result, err
			}
		}
		if p.Format == "raw" {
			if err = p.checkSources(); err != nil {
				return result, err
			}
		}
		update(progress)
		transferCtx, stop := context.WithCancel(ctx)
		timer := time.AfterFunc(30*time.Second, stop)
		_, err = call(transferCtx, map[string]any{"op": "rz", "stage": stage}, func(reader io.Reader, writer io.Writer) error {
			return sendUploadZMODEM(reader, writer, p, func() { timer.Stop() }, func(n int64) { progress.TransferredBytes = n; update(progress) })
		})
		timer.Stop()
		stop()
		if err != nil {
			if errors.Is(err, context.Canceled) && ctx.Err() == nil {
				return result, fault("ZMODEM_HANDSHAKE_TIMEOUT", "等待远端 rz 握手超时，接收进程已停止")
			}
			return result, err
		}
		if p.Format == "raw" {
			if err = p.checkSources(); err != nil {
				return result, err
			}
		}
	}
	progress.TransferredBytes = p.Size
	progress.Phase = "restoring"
	update(progress)
	params := metadata("restore")
	params["stage"] = stage
	params["destination_identity"] = destination.Identity
	body, err = call(ctx, params, nil)
	if err != nil {
		var known *Fault
		uncertain := remoteUnknown(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &known) && (known.Code == "REMOTE_PREPARATION_FAILED" || known.Code == "REMOTE_METADATA_INVALID"))
		if uncertain {
			for i := range result.Files {
				result.Files[i].Status = "unknown"
			}
			if !remoteUnknown(err) {
				err = fault("UPLOAD_RESULT_UNKNOWN", "远端进程已结束，但还原或发布结果未确认，请核对最终路径")
			}
		}
		return result, err
	}
	var restored struct {
		Files []UploadedFile `json:"files"`
		Error string         `json:"error"`
	}
	unknownResult := func(message string) (UploadResult, error) {
		for i := range result.Files {
			result.Files[i].Status = "unknown"
		}
		return result, fault("UPLOAD_RESULT_UNKNOWN", message)
	}
	if json.Unmarshal(body, &restored) != nil || len(restored.Files) != len(result.Files) {
		return unknownResult("远端发布结果无效，请核对最终路径")
	}
	for i, item := range restored.Files {
		if item.Path != result.Files[i].Path || item.Name != result.Files[i].Name || (item.Status != "completed" && item.Status != "failed" && item.Status != "not_published") {
			return unknownResult("远端发布结果与上传清单不符")
		}
		item.Source = result.Files[i].Source
		result.Files[i] = item
	}
	if restored.Error != "" {
		return result, fault(restored.Error, "远端部分或全部文件未能发布，请查看逐项结果")
	}
	if !uploadFilesReady(result.Files) {
		return result, fault("UPLOAD_RESULT_UNKNOWN", "未能确认全部文件已发布")
	}
	progress.Phase = "cleanup"
	ready := result
	for _, item := range owned {
		ready.RemoteArtifacts = append(ready.RemoteArtifacts, item.Path)
	}
	progress.Published = &ready
	update(progress)
	return result, nil
}

func isUploadPermissionError(err error) bool {
	var f *Fault
	return errors.As(err, &f) && f.Code == "UPLOAD_NOT_PERMITTED"
}

type uploadReadTracker struct {
	io.Reader
	err error
}

func (r *uploadReadTracker) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if err != nil && err != io.EOF {
		r.err = err
	}
	if err == io.EOF && n == 0 {
		r.err = io.ErrUnexpectedEOF
	}
	return n, err
}

func sendUploadZMODEM(reader io.Reader, writer io.Writer, p *preparedUpload, ready func(), progress func(int64)) error {
	r := bufio.NewReader(reader)
	window := []byte{}
	found := false
	for i := 0; i < 64<<10; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		window = append(window, b)
		if len(window) > 20 {
			window = window[1:]
		}
		if len(window) == 20 {
			if upload, valid := zmodem.InitialHeader(window); valid && upload {
				found = true
				break
			}
		}
	}
	if !found {
		return fault("ZMODEM_HANDSHAKE_FAILED", "未收到有效 rz 握手")
	}
	ready()
	skipped, err := zmodem.Send(r, writer, zmodem.Upload{Name: "payload", Size: p.Size, Reader: io.NewSectionReader(p.File, 0, p.Size)}, zmodem.SendOptions{Progress: func(_ string, _ int64, n int64, _ bool) { progress(n) }})
	if err == nil && skipped {
		return fault("UPLOAD_SKIPPED", "远端跳过了上传内容，未发布文件")
	}
	return err
}
