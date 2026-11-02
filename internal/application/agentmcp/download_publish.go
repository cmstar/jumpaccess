package agentmcp

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type publishedDownloadPath struct {
	name string
	info os.FileInfo
}

func removeOwnedDownloadDirectory(root *os.Root, name string, created os.FileInfo) error {
	current, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if created == nil || !current.IsDir() || !os.SameFile(created, current) {
		return fault("LOCAL_TEMPORARY_CHANGED", "本地临时目录身份已变化，未执行清理")
	}
	return root.RemoveAll(name)
}

// 在目标目录内完成还原与检查，再排他发布。公开路径只清理本次实际创建的对象。
func publishDownload(ctx context.Context, artifactFile *os.File, base string, a remoteArtifact, id string) (result DownloadResult, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if err = validateDownloadManifest(a); err != nil {
		return
	}
	if !filepath.IsAbs(base) {
		return result, fault("INVALID_LOCAL_DIRECTORY", "保存目录必须是绝对路径")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return result, err
	}
	defer root.Close()
	temporary := ".jumpaccess-download-" + rand.Text() + ".part"
	if err = root.Mkdir(temporary, 0700); err != nil {
		return
	}
	temporaryInfo, err := root.Lstat(temporary)
	if err != nil {
		result.Warnings = append(result.Warnings, "本地暂存位置无法确认，需手工检查："+filepath.Join(base, temporary))
		return
	}
	defer func() {
		if cleanupErr := removeOwnedDownloadDirectory(root, temporary, temporaryInfo); cleanupErr != nil {
			result.Warnings = append(result.Warnings, "本地暂存文件清理失败或身份已变化："+filepath.Join(base, temporary))
		}
	}()
	staging, err := root.OpenRoot(temporary)
	if err != nil {
		return result, err
	}
	files, restoreErr := restoreDownloadInto(ctx, artifactFile, staging, a)
	staging.Close()
	if restoreErr != nil {
		return result, restoreErr
	}
	if err = ctx.Err(); err != nil {
		return
	}
	top := a.RootName
	if top == "" {
		top = strings.SplitN(a.Entries[0].Name, "/", 2)[0]
	}
	for _, entry := range a.Entries {
		if strings.SplitN(entry.Name, "/", 2)[0] != top {
			return result, fault("INVALID_MANIFEST", "下载结果必须包含一个顶层对象")
		}
	}
	topInfo, err := root.Lstat(filepath.Join(temporary, top))
	if err != nil {
		return result, err
	}
	owned := []publishedDownloadPath{}
	published := false
	defer func() {
		if published {
			return
		}
		for i := len(owned) - 1; i >= 0; i-- {
			item := owned[i]
			if item.info == nil {
				result.Warnings = append(result.Warnings, "本地半成品身份无法确认，未清理："+filepath.Join(base, item.name))
				continue
			}
			current, statErr := root.Lstat(item.name)
			if os.IsNotExist(statErr) {
				continue
			}
			if statErr != nil || !os.SameFile(item.info, current) {
				result.Warnings = append(result.Warnings, "本地半成品已变化，未清理："+filepath.Join(base, item.name))
				continue
			}
			if cleanupErr := root.Remove(item.name); cleanupErr != nil {
				result.Warnings = append(result.Warnings, "本地半成品清理失败："+filepath.Join(base, item.name))
			}
		}
	}()
	finalName := top
	for suffix := 1; ; suffix++ {
		if err = ctx.Err(); err != nil {
			return
		}
		if suffix > 1 {
			finalName = downloadAlternateName(top, suffix)
		}
		if topInfo.IsDir() {
			err = root.Mkdir(finalName, 0700)
			if err == nil {
				err = recordPublishedDownload(root, finalName, &owned)
			}
		} else {
			err = publishDownloadFile(ctx, root, filepath.Join(temporary, top), finalName, &owned)
		}
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return
		}
		break
	}
	if topInfo.IsDir() {
		if err = publishDownloadDirectory(ctx, root, filepath.Join(temporary, top), finalName, &owned); err != nil {
			return
		}
	}
	result.Directory = base
	if topInfo.IsDir() {
		result.Directory = filepath.Join(base, finalName)
	}
	for i := range files {
		relative := strings.TrimPrefix(files[i].Path, top)
		files[i].Path = filepath.Join(base, finalName, filepath.FromSlash(strings.TrimPrefix(relative, "/")))
	}
	result.Files = files
	published = true
	return
}

func downloadAlternateName(original string, suffix int) string {
	ext := path.Ext(original)
	stem := strings.TrimSuffix(original, ext)
	return fmt.Sprintf("%s (%d)%s", stem, suffix, ext)
}

func recordPublishedDownload(root *os.Root, name string, owned *[]publishedDownloadPath) error {
	info, err := root.Lstat(name)
	*owned = append(*owned, publishedDownloadPath{name, info})
	return err
}

func publishDownloadDirectory(ctx context.Context, root *os.Root, source, destination string, owned *[]publishedDownloadPath) error {
	directory, err := root.Open(source)
	if err != nil {
		return err
	}
	children, readErr := directory.ReadDir(-1)
	directory.Close()
	if readErr != nil {
		return readErr
	}
	for _, child := range children {
		if err := ctx.Err(); err != nil {
			return err
		}
		from, to := filepath.Join(source, child.Name()), filepath.Join(destination, child.Name())
		if child.IsDir() {
			if err := root.Mkdir(to, 0700); err != nil {
				return err
			}
			if err := recordPublishedDownload(root, to, owned); err != nil {
				return err
			}
			if err = publishDownloadDirectory(ctx, root, from, to, owned); err != nil {
				return err
			}
		} else {
			if err := publishDownloadFile(ctx, root, from, to, owned); err != nil {
				return err
			}
		}
	}
	return nil
}

func publishDownloadFile(ctx context.Context, root *os.Root, source, destination string, owned *[]publishedDownloadPath) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sourceInfo, err := root.Lstat(source)
	if err != nil {
		return err
	}
	if !sourceInfo.Mode().IsRegular() {
		return fault("UNSAFE_ARCHIVE", "本地暂存文件类型发生变化")
	}
	if err := root.Link(source, destination); err == nil {
		*owned = append(*owned, publishedDownloadPath{destination, sourceInfo})
		return nil
	} else if os.IsExist(err) {
		return err
	}
	// 不支持硬链接的文件系统使用排他创建；从不以 Rename 覆盖已有对象。
	input, err := root.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := root.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	info, err := output.Stat()
	*owned = append(*owned, publishedDownloadPath{destination, info})
	if err != nil {
		output.Close()
		return err
	}
	_, copyErr := io.Copy(output, downloadContextReader{ctx, input})
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
