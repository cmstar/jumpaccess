package agentmcp

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type downloadContextReader struct {
	ctx context.Context
	io.Reader
}

func (r downloadContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
func validDownloadName(name string) bool {
	if name == "" || path.IsAbs(name) || path.Clean(name) != name || len(name) > 4096 || !utf8.ValidString(name) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 240 || strings.ContainsAny(part, `\:<>"|?*`) || strings.TrimRight(part, ". ") != part || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		runes := []rune(stem)
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(runes) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && strings.ContainsRune("0123456789¹²³", runes[3])) {
			return false
		}
	}
	return true
}

// 只接受准备阶段列出的普通文件和目录，解包不信任归档路径、类型或长度。
func restoreDownload(ctx context.Context, filename, destination string, a remoteArtifact) ([]DownloadedFile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	root, err := os.OpenRoot(destination)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return restoreDownloadInto(ctx, file, root, a)
}

// 链接交付也验证清单；此检查不读取或完整解压传输文件。
func validateDownloadManifest(a remoteArtifact) error {
	if len(a.Entries) == 0 || len(a.Entries) > 1001 || a.Size < 0 {
		return fault("INVALID_MANIFEST", "下载清单无效")
	}
	if a.Size > 1<<30 {
		return fault("TRANSFER_TOO_LARGE", "传输文件超过 1 GiB")
	}
	switch a.Format {
	case "raw", "gzip":
		if len(a.Entries) != 1 || a.Entries[0].Directory || strings.Contains(a.Entries[0].Name, "/") || a.Wrapped {
			return fault("INVALID_MANIFEST", "单文件下载清单无效")
		}
	case "tar.gz":
	default:
		return fault("COMPRESSION_UNAVAILABLE", "不支持的下载压缩格式")
	}
	if a.RootName != "" && (!validDownloadName(a.RootName) || strings.Contains(a.RootName, "/")) {
		return fault("UNSAFE_ARCHIVE", "下载顶层名称无效")
	}
	if a.SuggestedName != "" && (!validDownloadName(a.SuggestedName) || strings.Contains(a.SuggestedName, "/")) {
		return fault("UNSAFE_ARCHIVE", "建议保存名称无效")
	}
	seenNames := map[string]bool{}
	names := map[string]string{}
	directories := map[string]bool{}
	for _, entry := range a.Entries {
		if !validDownloadName(entry.Name) || seenNames[strings.ToLower(entry.Name)] || entry.Size < 0 || entry.Size > 1<<53 || (entry.Directory && entry.Size != 0) {
			return fault("UNSAFE_ARCHIVE", "清单包含不支持、冲突或不安全的文件名")
		}
		if a.RootName != "" && strings.SplitN(entry.Name, "/", 2)[0] != a.RootName {
			return fault("INVALID_MANIFEST", "下载内容与顶层名称不符")
		}
		seenNames[strings.ToLower(entry.Name)] = true
		directories[entry.Name] = entry.Directory
		for name := entry.Name; name != "."; name = path.Dir(name) {
			key := strings.ToLower(name)
			if previous, ok := names[key]; ok && previous != name {
				return fault("UNSAFE_ARCHIVE", "清单路径存在大小写冲突")
			}
			names[key] = name
		}
	}
	for _, entry := range a.Entries {
		for parent := path.Dir(entry.Name); parent != "."; parent = path.Dir(parent) {
			if directory, exists := directories[parent]; exists && !directory {
				return fault("UNSAFE_ARCHIVE", "清单文件被同时用作父目录")
			}
		}
	}
	if a.Wrapped && (a.RootName == "" || !directories[a.RootName]) {
		return fault("INVALID_MANIFEST", "多来源下载缺少结果目录")
	}
	return nil
}

func restoreDownloadInto(ctx context.Context, file *os.File, root *os.Root, a remoteArtifact) ([]DownloadedFile, error) {
	if err := validateDownloadManifest(a); err != nil {
		return nil, err
	}
	entries := make(map[string]remoteEntry, len(a.Entries))
	for _, entry := range a.Entries {
		entries[entry.Name] = entry
	}
	// 从同一文件句柄核对长度；内容直接进入还原过程，不预扫描暂存包。
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != a.Size {
		return nil, fault("INTEGRITY_FAILED", "传输文件长度校验失败")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	files := []DownloadedFile{}
	write := func(entry remoteEntry, reader io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(entry.Name)
		if entry.Directory {
			return root.MkdirAll(name, 0700)
		}
		if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return err
		}
		output, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(output, downloadContextReader{ctx, io.LimitReader(reader, entry.Size+1)})
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != entry.Size {
			return fault("INTEGRITY_FAILED", "还原文件长度校验失败")
		}
		files = append(files, DownloadedFile{Path: entry.Name, Source: entry.Source, Size: entry.Size, CapturedAt: entry.CapturedAt})
		return nil
	}
	var reader io.Reader = file
	if a.Format == "gzip" || a.Format == "tar.gz" {
		gz, err := gzip.NewReader(downloadContextReader{ctx, file})
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	switch a.Format {
	case "raw", "gzip":
		if len(a.Entries) != 1 || a.Entries[0].Directory {
			return nil, fault("INVALID_MANIFEST", "单文件下载清单无效")
		}
		if err := write(a.Entries[0], reader); err != nil {
			return nil, err
		}
	case "tar.gz":
		tr := tar.NewReader(reader)
		seen := map[string]bool{}
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			name := strings.TrimSuffix(header.Name, "/")
			entry, ok := entries[name]
			if !ok || seen[name] || !validDownloadName(name) {
				return nil, fault("UNSAFE_ARCHIVE", "归档包含清单外、重复或不安全的路径")
			}
			seen[name] = true
			if entry.Directory {
				if header.Typeflag != tar.TypeDir || header.Size != 0 {
					return nil, fault("UNSAFE_ARCHIVE", "归档目录类型无效")
				}
			} else if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size != entry.Size {
				return nil, fault("UNSAFE_ARCHIVE", "归档文件类型或长度无效")
			}
			if err := write(entry, tr); err != nil {
				return nil, err
			}
		}
		if len(seen) != len(entries) {
			return nil, fault("INTEGRITY_FAILED", "归档缺少清单中的文件")
		}
		// tar EOF 不代表 gzip 尾部校验已完成；只允许有界的块填充。
		tail, err := io.ReadAll(io.LimitReader(downloadContextReader{ctx, reader}, 1<<20))
		if err != nil {
			return nil, err
		}
		if len(tail) == 1<<20 {
			return nil, fault("UNSAFE_ARCHIVE", "归档尾部过长")
		}
		for _, b := range tail {
			if b != 0 {
				return nil, fault("UNSAFE_ARCHIVE", "归档尾部包含额外内容")
			}
		}
	default:
		return nil, fault("COMPRESSION_UNAVAILABLE", "不支持的下载压缩格式")
	}
	return files, nil
}
