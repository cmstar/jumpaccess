package downloadcache

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Pending struct {
	File             *os.File
	store            *Store
	name, suffix, id string
	size             int64
	finished         bool
}

func (s *Store) Begin(size int64, suffix string) (*Pending, error) {
	suffix = normalizeSuffix(suffix)
	if !validSuffix(suffix) || size < 0 {
		return nil, ErrInvalidName
	}
	s.mu.Lock()
	var cleanupErr error
	defer func() {
		s.mu.Unlock()
		s.warn(cleanupErr)
	}()
	if s.closed {
		return nil, ErrClosed
	}
	if size > s.options.MaxFileSize {
		return nil, ErrTooLarge
	}
	// 配额不足时先回收过期文件；清理失败的文件继续参与计数。
	cleanupErr = s.sweepLocked(false)
	var reserved int64
	for _, e := range s.files {
		if e.size > s.options.MaxBytes-reserved {
			return nil, ErrCapacity
		}
		reserved += e.size
	}
	if len(s.files) >= s.options.MaxFiles || size > s.options.MaxBytes || reserved > s.options.MaxBytes-size {
		return nil, ErrCapacity
	}
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return nil, err
	}
	uuid[6] = (uuid[6] & 15) | 64
	uuid[8] = (uuid[8] & 63) | 128
	id := fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	name := ".jumpaccess-download-pending-" + s.options.Now().UTC().Format(fileTimeLayout) + "-" + id + ".part"
	f, err := s.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		_ = s.root.Remove(name)
		return nil, err
	}
	p := &Pending{File: f, store: s, name: name, suffix: suffix, id: id, size: size}
	s.files[name] = &entry{size: size, pending: p, info: info}
	return p, nil
}

func (p *Pending) Commit(downloadName string) (Artifact, error) {
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Artifact{}, ErrClosed
	}
	if p.finished {
		return Artifact{}, fmt.Errorf("领取缓存写入已结束")
	}
	if !validDownloadName(downloadName) {
		return Artifact{}, ErrInvalidName
	}
	if err := p.File.Sync(); err != nil && !errors.Is(err, os.ErrClosed) {
		return Artifact{}, err
	}
	if err := p.File.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return Artifact{}, err
	}
	info, err := s.root.Lstat(p.name)
	if err != nil {
		return Artifact{}, err
	}
	e := s.files[p.name]
	if !info.Mode().IsRegular() || !os.SameFile(e.info, info) {
		return Artifact{}, ErrInvalidName
	}
	if info.Size() != p.size {
		if info.Size() > e.size {
			e.size = info.Size()
		}
		return Artifact{}, ErrSizeMismatch
	}
	// 文件名精度为秒，向上取整避免比承诺的保留时长提前失效。
	expires := s.options.Now().UTC().Add(s.options.TTL).Add(time.Second - 1).Truncate(time.Second)
	name := ".jumpaccess-download-exp-" + expires.Format(fileTimeLayout) + "-" + p.id + p.suffix
	if err = s.root.Rename(p.name, name); err != nil {
		return Artifact{}, err
	}
	p.finished = true
	e.pending = nil
	e.info = info
	delete(s.files, p.name)
	s.files[name] = e
	return Artifact{Name: name, DownloadName: downloadName, Size: p.size, ExpiresAt: expires}, nil
}

func (p *Pending) Abort() error {
	s := p.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.finished {
		return nil
	}
	p.finished = true
	err := p.File.Close()
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	if e := s.files[p.name]; e != nil {
		e.pending = nil
		e.stale = true
	}
	return errors.Join(err, s.deleteLocked(p.name))
}

// Import 复用已准备好的不可变传输包，不解压、不重新读取远端源文件。
func (s *Store) Import(ctx context.Context, source string, size int64, suffix, downloadName string) (artifact Artifact, err error) {
	if err = ctx.Err(); err != nil {
		return Artifact{}, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Artifact{}, err
	}
	if !info.Mode().IsRegular() {
		return Artifact{}, ErrInvalidName
	}
	f, err := os.Open(source)
	if err != nil {
		return Artifact{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return Artifact{}, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return Artifact{}, ErrInvalidName
	}
	if opened.Size() != size {
		return Artifact{}, ErrSizeMismatch
	}
	p, err := s.Begin(size, suffix)
	if err != nil {
		return Artifact{}, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.Abort())
		}
	}()
	_, err = io.Copy(p.File, &contextReader{ctx: ctx, reader: io.LimitReader(f, size+1)})
	if err != nil {
		return Artifact{}, err
	}
	if err = ctx.Err(); err != nil {
		return Artifact{}, err
	}
	return p.Commit(downloadName)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func normalizeSuffix(suffix string) string {
	if suffix == "" {
		return ".bin"
	}
	if !strings.HasPrefix(suffix, ".") {
		return "." + suffix
	}
	return suffix
}
