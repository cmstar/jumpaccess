package agentmcp

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type uploadSource struct {
	remoteEntry
	info os.FileInfo
}

type preparedUpload struct {
	File      *os.File
	Format    string
	Size      int64
	Entries   []remoteEntry
	sources   []uploadSource
	temporary bool
	identity  os.FileInfo
}

func (p *preparedUpload) Close() error {
	if p.File == nil {
		return nil
	}
	f := p.File
	p.File = nil
	err := f.Close()
	if p.temporary {
		current, statErr := os.Lstat(f.Name())
		if statErr == nil && os.SameFile(p.identity, current) {
			return errors.Join(err, os.Remove(f.Name()))
		}
		if !os.IsNotExist(statErr) {
			return errors.Join(err, fault("LOCAL_TEMPORARY_CHANGED", "上传准备包身份已变化，未执行清理"))
		}
	}
	return err
}

// 逐级固定目录身份，拒绝链接；最终文件也须与准备清单中的对象一致。
func openUploadSource(filename string) (_ *os.File, err error) {
	defer func() { err = uploadSourceError(err) }()
	volume := filepath.VolumeName(filename) + string(os.PathSeparator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, err
	}
	defer func() { root.Close() }()
	rel, err := filepath.Rel(volume, filename)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fault("UNSUPPORTED_PATH", "上传源路径无效")
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, part := range parts {
		info, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return nil, fault("UNSUPPORTED_FILE_TYPE", "上传只支持普通文件和目录，不支持链接")
		}
		if i == len(parts)-1 {
			file, err := root.Open(part)
			if err != nil {
				return nil, err
			}
			opened, err := file.Stat()
			if err != nil || !os.SameFile(info, opened) {
				file.Close()
				return nil, fault("SOURCE_CHANGED", "上传源文件在打开时已变化")
			}
			return file, nil
		}
		if !info.IsDir() {
			return nil, fault("UNSUPPORTED_PATH", "上传源路径父级不是目录")
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, fault("SOURCE_CHANGED", "上传源目录在打开时已变化")
		}
		root.Close()
		root = next
	}
	return nil, fault("UNSUPPORTED_PATH", "上传源路径无效")
}

func uploadSourceError(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fault("LOCAL_SOURCE_PERMISSION_DENIED", "MCP 服务没有源文件读取权限；本阶段仅支持本机直读，尚不提供 HTTP 中转")
	}
	if errors.Is(err, os.ErrNotExist) {
		return fault("LOCAL_SOURCE_NOT_FOUND", "MCP 服务电脑上的上传源文件或目录不存在")
	}
	return err
}

func prepareUpload(ctx context.Context, paths []string, limit int64) (_ *preparedUpload, err error) {
	p := &preparedUpload{}
	defer func() {
		if err != nil {
			_ = p.Close()
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if len(paths) == 0 || len(paths) > 100 {
		return nil, fault("INVALID_ARGUMENT", "上传需提供 1–100 个本机绝对路径")
	}
	unique := map[string]os.FileInfo{}
	seenPaths := map[string]bool{}
	for _, source := range paths {
		if !filepath.IsAbs(source) {
			return nil, fault("INVALID_ARGUMENT", "上传源须为服务电脑上的绝对路径")
		}
		source = filepath.Clean(source)
		key := source
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seenPaths[key] {
			continue
		}
		seenPaths[key] = true
		f, openErr := openUploadSource(source)
		if openErr != nil {
			return nil, openErr
		}
		info, statErr := f.Stat()
		f.Close()
		if statErr != nil {
			return nil, statErr
		}
		unique[source] = info
	}
	roots := []string{}
	for source := range unique {
		contained := false
		for parent, info := range unique {
			if parent == source || !info.IsDir() {
				continue
			}
			rel, relErr := filepath.Rel(parent, source)
			if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				contained = true
				break
			}
		}
		if !contained {
			roots = append(roots, source)
		}
	}
	sort.Strings(roots)
	names := map[string]bool{}
	var visit func(string, string) error
	visit = func(source, name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validDownloadName(name) {
			return fault("UNSUPPORTED_PATH", "上传文件名无法安全还原")
		}
		if len(p.sources) >= 1000 {
			return fault("TOO_MANY_FILES", "上传最多包含 1000 个文件或目录条目")
		}
		key := strings.ToLower(name)
		if names[key] {
			return fault("UPLOAD_NAME_CONFLICT", "上传来源存在同名或大小写冲突")
		}
		names[key] = true
		f, err := openUploadSource(source)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		entry := remoteEntry{Name: name, Source: source, Directory: info.IsDir()}
		if !entry.Directory {
			entry.Size = info.Size()
		}
		p.sources = append(p.sources, uploadSource{entry, info})
		p.Entries = append(p.Entries, entry)
		if info.IsDir() {
			// 多读一项即可判定超限，不先把任意大的目录读入内存。
			children, err := f.ReadDir(1001 - len(p.sources))
			if err != nil && err != io.EOF {
				return err
			}
			if len(children)+len(p.sources) > 1000 {
				return fault("TOO_MANY_FILES", "上传最多包含 1000 个文件或目录条目")
			}
			sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
			for _, child := range children {
				if err := visit(filepath.Join(source, child.Name()), path.Join(name, child.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, source := range roots {
		if err = visit(source, filepath.Base(source)); err != nil {
			return nil, err
		}
	}
	p.Format = "tar.gz"
	if len(p.sources) == 1 && !p.sources[0].Directory {
		p.Format = "gzip"
		for _, ext := range []string{".gz", ".zip", ".bz2", ".xz", ".7z", ".rar", ".zst", ".tgz", ".jar", ".war"} {
			if strings.HasSuffix(strings.ToLower(p.sources[0].Source), ext) {
				p.Format = "raw"
				break
			}
		}
	}
	if p.Format == "raw" {
		p.File, err = openUploadSource(p.sources[0].Source)
		if err != nil {
			return nil, err
		}
		opened, statErr := p.File.Stat()
		if statErr != nil || !sameUploadSource(p.sources[0].info, opened) {
			return nil, fault("SOURCE_CHANGED", "上传源内容在打开时已变化")
		}
		p.Size = p.sources[0].Size
		if p.Size > limit {
			return nil, fault("TRANSFER_TOO_LARGE", "实际上传文件超过 1 GiB")
		}
		if err = p.checkSources(); err != nil {
			return nil, err
		}
		return p, nil
	}
	p.File, err = os.CreateTemp("", ".jumpaccess-upload-*.part")
	if err != nil {
		return nil, err
	}
	p.temporary = true
	p.identity, err = p.File.Stat()
	if err != nil {
		return nil, err
	}
	w := &uploadLimitedWriter{Writer: p.File, remaining: limit}
	gz := gzip.NewWriter(w)
	var tw *tar.Writer
	if p.Format == "tar.gz" {
		tw = tar.NewWriter(gz)
	}
	for _, source := range p.sources {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if tw != nil {
			typeflag, mode := byte(tar.TypeReg), int64(0600)
			if source.Directory {
				typeflag, mode = tar.TypeDir, 0700
			}
			if err = tw.WriteHeader(&tar.Header{Name: source.Name, Size: source.Size, Mode: mode, Typeflag: typeflag}); err != nil {
				return nil, err
			}
		}
		if source.Directory {
			continue
		}
		f, openErr := openUploadSource(source.Source)
		if openErr != nil {
			return nil, openErr
		}
		info, statErr := f.Stat()
		if statErr != nil || !sameUploadSource(source.info, info) {
			f.Close()
			return nil, fault("SOURCE_CHANGED", "上传源内容已变化")
		}
		var dest io.Writer = gz
		if tw != nil {
			dest = tw
		}
		_, copyErr := io.CopyN(dest, downloadContextReader{ctx, f}, source.Size)
		info, statErr = f.Stat()
		f.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if statErr != nil || !sameUploadSource(source.info, info) {
			return nil, fault("SOURCE_CHANGED", "上传源内容在准备期间已变化")
		}
	}
	if tw != nil {
		if err = tw.Close(); err != nil {
			return nil, err
		}
	}
	if err = gz.Close(); err != nil {
		return nil, err
	}
	if err = p.checkSources(); err != nil {
		return nil, err
	}
	info, err := p.File.Stat()
	if err != nil {
		return nil, err
	}
	p.Size = info.Size()
	_, err = p.File.Seek(0, io.SeekStart)
	return p, err
}

func sameUploadSource(before, after os.FileInfo) bool {
	return after != nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && before.Mode() == after.Mode()
}

func (p *preparedUpload) checkSources() error {
	for _, source := range p.sources {
		info, err := os.Lstat(source.Source)
		if err != nil || !sameUploadSource(source.info, info) {
			return fault("SOURCE_CHANGED", "上传源内容在准备或传输期间已变化")
		}
	}
	return nil
}

type uploadLimitedWriter struct {
	io.Writer
	remaining int64
}

func (w *uploadLimitedWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, fault("TRANSFER_TOO_LARGE", "准备后的上传文件超过 1 GiB")
	}
	n, err := w.Writer.Write(b)
	w.remaining -= int64(n)
	return n, err
}
