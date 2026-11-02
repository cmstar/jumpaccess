package agentmcp

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"strings"

	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
	"github.com/cmstar/jumpaccess/internal/config"
)

// 交付规则取自本次运行配置，不根据单个请求的来源猜测调用方文件系统。
func DownloadDeliveryMode(cfg config.MCP) string {
	ip := net.ParseIP(cfg.ListenHost)
	if !cfg.AllowLAN || cfg.ListenHost == "localhost" || ip.IsLoopback() {
		return "local"
	}
	return "link"
}

const localDownloadInstructions = "文件已保存并完成解压和必要检查，可直接使用。无需再次解压、计算内容哈希、rsync 或回到远端逐文件校验。"

func preparedDownloadLink(a remoteArtifact, artifact downloadcache.Artifact) DownloadResult {
	instructions := "文件已在服务端准备完成，尚未保存到调用方设备。通过 link.url 领取完整文件；保存或解压时同名另存，不覆盖已有文件、不合并已有目录。HTTP 中断时在有效期内重新领取整包。无需计算内容哈希、rsync 或回到远端逐文件校验。"
	if a.Format != "raw" {
		instructions += "领取后仅解开本次新增的压缩层，无需独立测试压缩包；成功解压后仅清理本次领取的传输包，用户要求保留则保留，不删除既有文件。解压失败时保留包并报告错误。原本就是压缩文件的目标不继续解压。"
	} else {
		instructions += "领取到的就是目标文件，无需解压。"
	}
	return DownloadResult{Delivery: "link", Instructions: instructions, Link: &DownloadLink{
		FileName: a.SuggestedName, Format: a.Format, Size: a.Size, ExpiresAt: artifact.ExpiresAt,
		RequiresExtraction: a.Format != "raw", RestoreName: a.RootName, Artifact: artifact, Available: true,
	}}
}

func downloadRenames(a remoteArtifact) []DownloadRename {
	if !a.Wrapped {
		return nil
	}
	var renamed []DownloadRename
	for _, entry := range a.Entries {
		name := strings.TrimPrefix(entry.Name, a.RootName+"/")
		if entry.Source != "" && !strings.Contains(name, "/") && name != path.Base(entry.Source) {
			renamed = append(renamed, DownloadRename{Source: entry.Source, ArchivePath: entry.Name})
		}
	}
	return renamed
}

// 只有接收目标的写入错误可触发交付回退，远端 PermissionDenied 不等于本地无权限。
type downloadLocalWriteError struct{ error }

func (e *downloadLocalWriteError) Unwrap() error { return e.error }

type downloadDestination struct {
	File     *os.File
	Pending  *downloadcache.Pending
	FellBack bool
}

func (d *downloadDestination) Close() error {
	if d.Pending != nil {
		return d.Pending.Abort()
	}
	if d.File == nil {
		return nil
	}
	err := d.File.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// 回退只更换接收位置，receive 始终使用已准备好的同一远端包和当前协议。
func receivePreparedDownload(ctx context.Context, cache *downloadcache.Store, a remoteArtifact, link bool, openLocal func() (*os.File, error), receive func(*os.File) error) (d *downloadDestination, err error) {
	d = &downloadDestination{}
	if err = ctx.Err(); err != nil {
		return
	}
	beginCache := func() error {
		if cache == nil {
			return fault("DOWNLOAD_CACHE_UNAVAILABLE", "下载领取缓存未配置")
		}
		pending, beginErr := cache.Begin(a.Size, downloadArtifactSuffix(a))
		if beginErr != nil {
			return downloadCacheError(beginErr)
		}
		d.Pending, d.File = pending, pending.File
		return nil
	}
	if link {
		err = beginCache()
	} else {
		d.File, err = openLocal()
		if errors.Is(err, os.ErrPermission) && ctx.Err() == nil {
			d.FellBack = true
			err = beginCache()
		}
	}
	if err != nil {
		return
	}
	err = receive(d.File)
	var writeErr *downloadLocalWriteError
	if err != nil && d.Pending == nil && errors.As(err, &writeErr) && errors.Is(writeErr, os.ErrPermission) && ctx.Err() == nil && !remoteUnknown(err) {
		_ = d.File.Close()
		d.FellBack = true
		if err = beginCache(); err != nil {
			return
		}
		err = receive(d.File)
	}
	return
}

// 已完成接收的文件始终通过现有句柄导入，目标目录权限改变不要求重新打开原路径。
func cacheOpenDownload(ctx context.Context, cache *downloadcache.Store, file *os.File, a remoteArtifact) (artifact downloadcache.Artifact, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if cache == nil {
		return artifact, fault("DOWNLOAD_CACHE_UNAVAILABLE", "下载领取缓存未配置")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return
	}
	pending, err := cache.Begin(a.Size, downloadArtifactSuffix(a))
	if err != nil {
		return artifact, downloadCacheError(err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, pending.Abort())
		}
	}()
	_, err = io.Copy(pending.File, downloadContextReader{ctx, io.LimitReader(file, a.Size+1)})
	if err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	return pending.Commit(a.SuggestedName)
}

func downloadArtifactSuffix(a remoteArtifact) string {
	switch a.Format {
	case "gzip":
		return ".gz"
	case "tar.gz":
		return ".tar.gz"
	default:
		// 原始文件保持下载时的友好名称；缓存内部固定扩展名不参与恢复。
		return ".bin"
	}
}

func downloadCacheError(err error) error {
	var existing *Fault
	if errors.As(err, &existing) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch {
	case errors.Is(err, downloadcache.ErrTooLarge):
		return fault("DOWNLOAD_SIZE_LIMIT", "准备后的传输文件超过 1 GiB")
	case errors.Is(err, downloadcache.ErrCapacity):
		return fault("DOWNLOAD_CACHE_FULL", "下载领取缓存已达到 8 GiB 或 50 个文件上限，请稍后重试")
	default:
		return fault("DOWNLOAD_CACHE_UNAVAILABLE", "下载领取缓存不可用，未生成下载链接")
	}
}

func normalizeArtifactNames(a *remoteArtifact) {
	if a.RootName == "" && len(a.Entries) > 0 {
		a.RootName = a.Entries[0].Name
	}
	if a.SuggestedName == "" {
		a.SuggestedName = path.Base(a.RootName)
		if a.Format != "raw" {
			a.SuggestedName += downloadArtifactSuffix(*a)
		}
	}
}
