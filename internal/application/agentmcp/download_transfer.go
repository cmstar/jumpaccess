package agentmcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
	"github.com/cmstar/jumpaccess/internal/downloads"
	"github.com/cmstar/jumpaccess/internal/zmodem"
)

type DownloadRemote interface {
	Open(string) (io.ReadCloser, error)
	Close() error
}
type DownloadSFTP struct {
	Client  DownloadRemote
	Root    *string
	Account string
}
type downloadCapability struct {
	Expires           time.Time
	Physical, Virtual string
	SZ                bool
	Home, Account     string
	SFTPUnavailable   bool
}
type stagingLocation struct{ physical, virtual string }

func (e *Engine) cachedDownloadCapability(target ResolvedTarget) (downloadCapability, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.downloadCapabilities[target]
	if ok && !e.opts.Now().Before(v.Expires) {
		delete(e.downloadCapabilities, target)
		ok = false
	}
	return v, ok
}
func (e *Engine) cacheDownloadCapability(target ResolvedTarget, v downloadCapability) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.downloadCapabilities) >= 256 {
		var oldest ResolvedTarget
		var at time.Time
		for key, item := range e.downloadCapabilities {
			if at.IsZero() || item.Expires.Before(at) {
				oldest, at = key, item.Expires
			}
		}
		delete(e.downloadCapabilities, oldest)
	}
	if v.Expires.IsZero() || !e.opts.Now().Before(v.Expires) {
		v.Expires = e.opts.Now().Add(10 * time.Minute)
	}
	e.downloadCapabilities[target] = v
}

func downloadStagingCandidates(root *string, home, account string, cached downloadCapability) []stagingLocation {
	candidates := []stagingLocation{}
	if cached.Physical != "" {
		candidates = append(candidates, stagingLocation{cached.Physical, cached.Virtual})
	}
	if root != nil {
		r := *root
		switch strings.ToLower(r) {
		case "", "~", "home":
			r = home
		}
		r = strings.ReplaceAll(r, "${HOME}", home)
		r = strings.ReplaceAll(r, "${ACCOUNT}", account)
		if path.IsAbs(r) && !strings.ContainsAny(r, "$\x00") {
			r = path.Clean(r)
			if r == "/" {
				candidates = append(candidates, stagingLocation{"/tmp", "/tmp"}, stagingLocation{home, home})
			} else {
				candidates = append(candidates, stagingLocation{r, "/"})
			}
		}
	}
	// 配置缺失或含未知变量时，仅以实际随机文件比对验证候选，不推断 chroot。
	candidates = append(candidates, stagingLocation{home, "/"}, stagingLocation{"/tmp", "/"})
	seen := map[stagingLocation]bool{}
	result := []stagingLocation{}
	for _, v := range candidates {
		if path.IsAbs(v.physical) && path.IsAbs(v.virtual) && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func (e *Engine) performDownload(ctx context.Context, target ResolvedTarget, in DownloadInput, update func(DownloadProgress)) (result DownloadResult, err error) {
	mode := e.opts.DeliveryMode
	if mode != "link" {
		mode = "local"
	}
	result.Delivery = mode
	base := in.LocalDirectory
	if mode == "local" && base == "" {
		base, err = downloads.Directory()
		if err != nil {
			return result, fault("LOCAL_DIRECTORY_UNAVAILABLE", "无法取得系统下载目录，请指定 local_directory")
		}
	}
	uuidBytes := make([]byte, 16)
	if _, err = rand.Read(uuidBytes); err != nil {
		return result, err
	}
	uuidBytes[6] = (uuidBytes[6] & 15) | 64
	uuidBytes[8] = (uuidBytes[8] & 63) | 128
	id := time.Now().UTC().Format("20060102T150405Z") + "-" + fmt.Sprintf("%x-%x-%x-%x-%x", uuidBytes[:4], uuidBytes[4:6], uuidBytes[6:8], uuidBytes[8:10], uuidBytes[10:])
	tempName := ".jumpaccess-download-" + id + ".part"
	var tempRoot *os.Root
	if mode == "local" {
		baseRoot, openErr := os.OpenRoot(base)
		if openErr == nil {
			defer baseRoot.Close()
			openErr = baseRoot.Mkdir(tempName, 0700)
			if openErr == nil {
				created, statErr := baseRoot.Lstat(tempName)
				defer func() {
					if cleanupErr := removeOwnedDownloadDirectory(baseRoot, tempName, created); cleanupErr != nil {
						result.Warnings = append(result.Warnings, "本地临时文件清理失败或身份已变化："+filepath.Join(base, tempName))
					}
				}()
				openErr = statErr
				if openErr == nil {
					tempRoot, openErr = baseRoot.OpenRoot(tempName)
				}
			}
		}
		if openErr != nil {
			if !errors.Is(openErr, os.ErrPermission) {
				return result, fault("LOCAL_DIRECTORY_UNAVAILABLE", "本地保存目录不存在、不可用或无法写入；不会创建目标目录")
			}
			mode, result.Delivery = "link", "link"
			result.Warnings = append(result.Warnings, "服务没有目标目录的写入权限，改为下载链接；请在调用方保存")
		} else {
			defer tempRoot.Close()
		}
	}
	if mode == "link" && e.opts.DownloadCache == nil {
		return result, fault("DOWNLOAD_CACHE_UNAVAILABLE", "下载领取缓存未配置")
	}
	if e.opts.Dial == nil {
		return result, fault("DOWNLOAD_UNAVAILABLE", "SSH 下载连接未配置")
	}
	connectCtx, cancel := context.WithTimeout(ctx, e.opts.Limits.ConnectTimeout)
	client, err := e.opts.Dial(connectCtx, target)
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
		if keepRemote {
			for _, file := range owned {
				result.RemoteArtifacts = append(result.RemoteArtifacts, file.Path)
			}
			result.Warnings = append(result.Warnings, "未能确认远端进程已结束，保留任务临时文件")
			return
		}
		cleanCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _, cleanErr := remoteDownloadCall(cleanCtx, client, map[string]any{"op": "cleanup", "files": owned}, nil)
		if cleanErr != nil {
			for _, file := range owned {
				result.RemoteArtifacts = append(result.RemoteArtifacts, file.Path)
			}
			result.Warnings = append(result.Warnings, "远端临时文件清理未确认，请检查 remote_artifacts")
		}
	}()
	call := func(callCtx context.Context, p map[string]any, consume func(io.Reader, io.Writer) error) (json.RawMessage, error) {
		body, created, callErr := remoteDownloadCall(callCtx, client, p, consume)
		owned = append(owned, created...)
		if remoteUnknown(callErr) {
			keepRemote = true
		}
		return body, callErr
	}
	var environment struct {
		Home    string `json:"home"`
		Account string `json:"account"`
		SZ      bool   `json:"sz"`
	}
	capability, cached := e.cachedDownloadCapability(target)
	var body json.RawMessage
	if cached {
		environment.Home, environment.Account, environment.SZ = capability.Home, capability.Account, capability.SZ
	} else {
		probeCtx, stopProbe := context.WithTimeout(ctx, 30*time.Second)
		body, err = call(probeCtx, map[string]any{"op": "environment"}, nil)
		stopProbe()
		if err != nil {
			return result, err
		}
		if json.Unmarshal(body, &environment) != nil || !path.IsAbs(environment.Home) {
			return result, fault("REMOTE_ENVIRONMENT_UNAVAILABLE", "无法确定远端账号环境")
		}
		capability.SZ, capability.Home, capability.Account = environment.SZ, environment.Home, environment.Account
		e.cacheDownloadCapability(target, capability)
	}
	// 一个 probe 在验证后立即删除。删除不确定则保留精确位置，不扫描历史文件。
	probe := func(location stagingLocation, sftp DownloadRemote) error {
		value := rand.Text()
		name := ".jumpaccess-probe-" + id + "-" + rand.Text() + ".tmp"
		probeCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		count := len(owned)
		_, probeErr := call(probeCtx, map[string]any{"op": "probe", "stage": location.physical, "name": name, "value": value, "paths": in.RemotePaths}, nil)
		if probeErr == nil && sftp != nil {
			stopRead := context.AfterFunc(probeCtx, func() { sftp.Close() })
			reader, openErr := sftp.Open(path.Join(location.virtual, name))
			if openErr != nil {
				probeErr = openErr
			} else {
				actual, readErr := io.ReadAll(io.LimitReader(reader, 1024))
				reader.Close()
				if readErr != nil {
					probeErr = readErr
				} else if string(actual) != value {
					probeErr = fault("SFTP_ROOT_UNCONFIRMED", "SSH 与 SFTP 暂存目录不对应")
				}
			}
			stopRead()
		}
		if len(owned) > count && !keepRemote {
			cleanupCtx, stopClean := context.WithTimeout(context.Background(), 5*time.Second)
			_, _, cleanErr := remoteDownloadCall(cleanupCtx, client, map[string]any{"op": "cleanup", "files": owned[count:]}, nil)
			stopClean()
			if cleanErr == nil {
				owned = owned[:count]
			}
		}
		return probeErr
	}
	var sftp DownloadSFTP
	var location stagingLocation
	sftpReason := "SFTP 连接不可用"
	if e.opts.OpenSFTP != nil && !capability.SFTPUnavailable {
		sftp, err = e.opts.OpenSFTP(ctx, target)
		if isDownloadIdentityError(err) {
			return result, err
		}
		var protocolFault *Fault
		if errors.As(err, &protocolFault) && protocolFault.Code == "SFTP_NOT_PERMITTED" {
			capability.SFTPUnavailable = true
			e.cacheDownloadCapability(target, capability)
		}
		if err == nil && sftp.Client != nil {
			defer sftp.Client.Close()
			for _, candidate := range downloadStagingCandidates(sftp.Root, environment.Home, sftp.Account, capability) {
				if probeErr := probe(candidate, sftp.Client); probeErr == nil {
					location = candidate
					break
				} else if keepRemote || ctx.Err() != nil || fatalDownloadProbe(probeErr) {
					return result, probeErr
				}
			}
			if location.physical == "" {
				sftpReason = "无法验证 SSH 与 SFTP 共用的暂存目录"
				sftp.Client.Close()
				sftp.Client = nil
			}
		}
	}
	protocol := "sftp"
	if location.physical == "" {
		protocol = "zmodem"
		result.Warnings = append(result.Warnings, sftpReason+"，尝试 ZMODEM")
		if !environment.SZ {
			return result, fault("NO_DOWNLOAD_PROTOCOL", sftpReason+"；远端未提供 sz")
		}
		for _, base := range []string{environment.Home, "/tmp"} {
			candidate := stagingLocation{physical: base}
			if probeErr := probe(candidate, nil); probeErr == nil {
				location = candidate
				break
			} else if keepRemote || ctx.Err() != nil || fatalDownloadProbe(probeErr) {
				return result, probeErr
			}
		}
		if location.physical == "" {
			return result, fault("STAGING_UNAVAILABLE", "没有可写的远端暂存目录")
		}
	}
	capability.Physical, capability.Virtual = location.physical, location.virtual
	e.cacheDownloadCapability(target, capability)
	update(DownloadProgress{Phase: "compressing", Protocol: protocol})
	body, err = call(ctx, map[string]any{"op": "prepare", "paths": in.RemotePaths, "stage": location.physical, "stem": ".jumpaccess-download-" + id, "result_name": "JumpAccess-" + id, "max_bytes": downloadcache.MaxArtifactSize}, nil)
	if err != nil {
		return result, err
	}
	var artifact remoteArtifact
	if json.Unmarshal(body, &artifact) != nil || artifact.Size < 0 || artifact.Size >= math.MaxInt64 || path.Dir(artifact.Path) != location.physical || !strings.HasPrefix(path.Base(artifact.Path), ".jumpaccess-download-"+id+".") {
		return result, fault("INVALID_MANIFEST", "远端传输文件清单无效")
	}
	if artifact.Size > downloadcache.MaxArtifactSize {
		return result, fault("TRANSFER_TOO_LARGE", "准备后的传输文件超过 1 GiB")
	}
	normalizeArtifactNames(&artifact)
	if err = validateDownloadManifest(artifact); err != nil {
		return result, err
	}
	if artifact.CompressionSkipped {
		result.Warnings = append(result.Warnings, "远端缺少 gzip 压缩能力，已准备有限长度的原始文件")
	}
	progress := DownloadProgress{Phase: "transferring", Protocol: protocol, TransferBytes: artifact.Size}
	update(progress)
	receive := func(local *os.File) error {
		progress.TransferredBytes = 0
		update(progress)
		write := &downloadWriter{file: local, update: func(n int64) { progress.TransferredBytes = n; update(progress) }}
		// 局部错误变量不能覆盖外层最终结果；仅本地写错误允许更换接收位置。
		var err error
		if protocol == "sftp" {
			stopRead := context.AfterFunc(ctx, func() { sftp.Client.Close() })
			reader, openErr := sftp.Client.Open(path.Join(location.virtual, path.Base(artifact.Path)))
			if openErr == nil {
				var copied int64
				copied, err = io.Copy(write, downloadContextReader{ctx, io.LimitReader(reader, artifact.Size+1)})
				reader.Close()
				if err == nil && copied != artifact.Size {
					err = io.ErrUnexpectedEOF
				}
			} else {
				err = openErr
			}
			stopRead()
			if err != nil && write.err == nil && ctx.Err() == nil {
				// 关闭本任务的 SFTP 连接后，才通过另一协议发送原封不动的暂存文件。
				sftp.Client.Close()
				capability.Physical, capability.Virtual = "", ""
				e.cacheDownloadCapability(target, capability)
				if !environment.SZ {
					return fault("NO_DOWNLOAD_PROTOCOL", "SFTP 传输失败；远端未提供 sz")
				}
				result.Warnings = append(result.Warnings, "SFTP 传输失败，已切换 ZMODEM 传输同一份内容")
				protocol = "zmodem"
				progress.Protocol = protocol
				if err = local.Truncate(0); err != nil {
					return &downloadLocalWriteError{err}
				}
				if _, err = local.Seek(0, io.SeekStart); err != nil {
					return &downloadLocalWriteError{err}
				}
				write.bytes = 0
				progress.TransferredBytes = 0
				update(progress)
			} else if err != nil {
				if write.err != nil {
					return &downloadLocalWriteError{write.err}
				}
				return err
			}
		}
		if protocol == "zmodem" {
			if artifact.Size > math.MaxUint32 {
				return fault("ZMODEM_SIZE_LIMIT", "SFTP 不可用，且实际传输文件达到 4 GiB，无法使用 ZMODEM")
			}
			transferCtx, stopTransfer := context.WithCancel(ctx)
			handshakeTimer := time.AfterFunc(30*time.Second, stopTransfer)
			_, err = call(transferCtx, map[string]any{"op": "sz", "path": artifact.Path}, func(reader io.Reader, writer io.Writer) error {
				return receiveDownloadZMODEM(reader, writer, artifact, write, func() { handshakeTimer.Stop() })
			})
			handshakeExpired := transferCtx.Err() != nil && ctx.Err() == nil
			handshakeTimer.Stop()
			stopTransfer()
			if handshakeExpired && !remoteUnknown(err) {
				err = fault("ZMODEM_HANDSHAKE_FAILED", "sz 握手超时，已确认停止本次传输")
			}
			if err != nil {
				// 远端停止未确认时不能重传，也不能让本地写错误掩盖远端未知状态。
				if write.err != nil && !remoteUnknown(err) {
					return &downloadLocalWriteError{write.err}
				}
				if !remoteUnknown(err) && ctx.Err() == nil {
					e.mu.Lock()
					delete(e.downloadCapabilities, target)
					e.mu.Unlock()
				}
				return err
			}
		}
		info, statErr := local.Stat()
		if statErr != nil {
			return &downloadLocalWriteError{statErr}
		}
		if info.Size() != artifact.Size {
			return fault("TRANSFER_SIZE_MISMATCH", "接收文件的字节长度与准备结果不符")
		}
		// 最终文件还原和缓存 Commit 各自负责 Sync；不重复同步内部传输副本。
		return ctx.Err()
	}
	destination, err := receivePreparedDownload(ctx, e.opts.DownloadCache, artifact, mode == "link", func() (*os.File, error) {
		return tempRoot.OpenFile("artifact", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	}, receive)
	defer func() {
		if closeErr := destination.Close(); closeErr != nil {
			result.Warnings = append(result.Warnings, "下载接收文件关闭或缓存半成品清理失败，将继续尝试清理")
		}
	}()
	if destination.FellBack {
		result.Warnings = append(result.Warnings, "服务没有目标目录的写入权限，改为下载链接；请在调用方保存")
	}
	if err != nil {
		return result, err
	}
	local, pending := destination.File, destination.Pending
	progress.Phase = "verifying"
	progress.TransferredBytes = artifact.Size
	update(progress)
	if err = ctx.Err(); err != nil {
		return result, err
	}
	var delivered DownloadResult
	if pending != nil {
		cached, commitErr := pending.Commit(artifact.SuggestedName)
		if commitErr != nil {
			return result, downloadCacheError(commitErr)
		}
		delivered = preparedDownloadLink(artifact, cached)
	} else {
		if _, err = local.Seek(0, io.SeekStart); err != nil {
			return result, err
		}
		progress.Phase = "extracting"
		update(progress)
		delivered, err = publishDownload(ctx, local, base, artifact, id)
		if err != nil {
			result.Warnings = append(result.Warnings, delivered.Warnings...)
			if !errors.Is(err, os.ErrPermission) || ctx.Err() != nil {
				return result, err
			}
			if e.opts.DownloadCache == nil {
				return result, fault("DOWNLOAD_CACHE_UNAVAILABLE", "目标目录权限不足，且下载领取缓存未配置")
			}
			cached, importErr := cacheOpenDownload(ctx, e.opts.DownloadCache, local, artifact)
			if importErr != nil {
				return result, downloadCacheError(importErr)
			}
			delivered = preparedDownloadLink(artifact, cached)
			result.Warnings = append(result.Warnings, "服务没有目标目录的写入权限，改为下载链接；请在调用方保存")
		} else {
			delivered.Delivery = "local"
			delivered.Instructions = localDownloadInstructions
		}
	}
	delivered.Warnings = append(result.Warnings, delivered.Warnings...)
	delivered.RenamedEntries = downloadRenames(artifact)
	result = delivered
	progress.Phase = "cleanup"
	ready := result
	for _, file := range owned {
		ready.RemoteArtifacts = append(ready.RemoteArtifacts, file.Path)
	}
	progress.Published = &ready
	update(progress)
	return result, nil
}

func fatalDownloadProbe(err error) bool {
	var f *Fault
	return errors.As(err, &f) && (f.Code == "SOURCE_UNREADABLE" || f.Code == "UNSUPPORTED_FILE_TYPE" || f.Code == "UNSUPPORTED_PATH")
}

type downloadWriter struct {
	file   io.Writer
	bytes  int64
	err    error
	last   time.Time
	update func(int64)
}

func (w *downloadWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.bytes += int64(n)
	if err != nil {
		w.err = err
	}
	if time.Since(w.last) >= 200*time.Millisecond {
		w.last = time.Now()
		w.update(w.bytes)
	}
	return n, err
}
func receiveDownloadZMODEM(reader io.Reader, writer io.Writer, a remoteArtifact, destination io.Writer, ready func()) error {
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
			upload, valid := zmodem.InitialHeader(window)
			if valid && !upload {
				found = true
				break
			}
		}
	}
	if !found {
		return fault("ZMODEM_HANDSHAKE_FAILED", "未收到有效 sz 握手")
	}
	ready()
	received, complete := false, false
	err := zmodem.Receive(r, writer, zmodem.ReceiveOptions{Open: func(name string, size int64) (zmodem.Download, error) {
		if received || name != path.Base(a.Path) || size != a.Size {
			return zmodem.Download{}, fault("ZMODEM_METADATA_INVALID", "sz 提供的文件与本次暂存文件不一致")
		}
		received = true
		return zmodem.Download{Write: func(p []byte) error {
			n, err := destination.Write(p)
			if err == nil && n != len(p) {
				err = io.ErrShortWrite
			}
			return err
		}, Close: func(ok bool) error { complete = ok; return nil }}, nil
	}})
	if err != nil {
		return err
	}
	if !received || !complete {
		return errors.New("ZMODEM 未收到完整文件")
	}
	return nil
}
