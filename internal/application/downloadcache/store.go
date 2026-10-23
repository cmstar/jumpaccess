// Package downloadcache 管理 MCP 领取文件；文件名保存有效期，授权密钥仅存在于本次服务内存中。
package downloadcache

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/filelock"
)

const (
	MaxArtifactSize int64 = 1 << 30
	RoutePrefix           = "/mcp/downloads/"
	fileTimeLayout        = "20060102T150405Z"
)

var (
	ErrTooLarge     = errors.New("传输文件超过大小限制")
	ErrCapacity     = errors.New("领取缓存额度不足")
	ErrBusy         = errors.New("HTTP 领取并发已满")
	ErrExpired      = errors.New("下载链接已过期")
	ErrNotFound     = errors.New("领取文件不存在")
	ErrInvalidToken = errors.New("下载链接凭据无效")
	ErrInvalidName  = errors.New("领取文件名无效")
	ErrClosed       = errors.New("领取缓存已停止")
	ErrOwned        = errors.New("领取缓存已由其他实例管理")
	ErrSizeMismatch = errors.New("传输文件大小与预期不符")
)

type Options struct {
	Directory                                   string
	Now                                         func() time.Time
	TTL, ScanInterval, LockTimeout              time.Duration
	MaxFileSize, MaxBytes                       int64
	MaxFiles, MaxTransfers, MaxTransfersPerFile int
	// OnCleanupError 接收后台清理失败；失败文件仍占用额度。
	OnCleanupError func(error)
}

type Artifact struct {
	Name         string
	DownloadName string
	Size         int64
	ExpiresAt    time.Time
}

type entry struct {
	size              int64
	pending           *Pending
	leases, transfers int
	stale             bool
	info              os.FileInfo
}

type Store struct {
	mu                sync.Mutex
	directory         string
	root              *os.Root
	options           Options
	secret            [32]byte
	files             map[string]*entry
	transfers, leases int
	closed            bool
	done              chan struct{}
	unlock            func() error
	remove            func(string) error
	closeErr          error
}

func New(options Options) (*Store, error) {
	if options.Directory == "" {
		return nil, fmt.Errorf("领取缓存目录不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.TTL == 0 {
		options.TTL = 30 * time.Minute
	}
	if options.ScanInterval == 0 {
		options.ScanInterval = time.Minute
	}
	if options.LockTimeout == 0 {
		options.LockTimeout = 200 * time.Millisecond
	}
	if options.MaxFileSize == 0 {
		options.MaxFileSize = MaxArtifactSize
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = 8 << 30
	}
	if options.MaxFiles == 0 {
		options.MaxFiles = 50
	}
	if options.MaxTransfers == 0 {
		options.MaxTransfers = 4
	}
	if options.MaxTransfersPerFile == 0 {
		options.MaxTransfersPerFile = 2
	}
	if options.TTL <= 0 || options.ScanInterval <= 0 || options.LockTimeout <= 0 || options.MaxFileSize < 0 || options.MaxBytes < 0 || options.MaxFiles < 0 || options.MaxTransfers < 0 || options.MaxTransfersPerFile < 0 {
		return nil, fmt.Errorf("领取缓存限制必须为正数")
	}
	directory, err := filepath.Abs(options.Directory)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("领取缓存必须是普通目录")
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.LockTimeout)
	defer cancel()
	unlock, err := (filelock.Locker{Dir: directory}).Lock(ctx, "ownership")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOwned, err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		_ = unlock()
		return nil, err
	}
	s := &Store{directory: directory, root: root, options: options, files: map[string]*entry{}, done: make(chan struct{}), unlock: unlock}
	s.remove = root.Remove
	if _, err = rand.Read(s.secret[:]); err != nil {
		_ = root.Close()
		_ = unlock()
		return nil, err
	}
	if err = s.sweepLocked(true); err != nil {
		s.warn(err)
	}
	go s.run()
	return s, nil
}

func (s *Store) Done() <-chan struct{} { return s.done }

func (s *Store) run() {
	ticker := time.NewTicker(s.options.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if err := s.Sweep(); err != nil && !errors.Is(err, ErrClosed) {
				s.warn(err)
			}
		}
	}
}

func (s *Store) warn(err error) {
	if err != nil && s.options.OnCleanupError != nil {
		s.options.OnCleanupError(err)
	}
}

// Sweep 只处理受控名称的普通文件；活动写入或领取不会被后台清理。
func (s *Store) Sweep() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return s.sweepLocked(false)
}

func (s *Store) sweepLocked(all bool) error {
	directory, err := s.root.Open(".")
	if err != nil {
		return err
	}
	items, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	var errs []error
	for _, item := range items {
		name := item.Name()
		kind, expiry, ok := parseName(name)
		if !ok {
			continue
		}
		info, err := s.root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		e := s.files[name]
		if e == nil {
			e = &entry{size: info.Size(), stale: true, info: info}
			s.files[name] = e
		}
		if e.pending != nil || e.leases > 0 {
			continue
		}
		// 启动遗留和未知所属任务的 part 均不恢复授权。
		if all || e.stale || kind == "pending" || !s.options.Now().Before(expiry) {
			if err := s.deleteLocked(name); err != nil {
				errs = append(errs, err)
			}
		}
	}
	// 文件已被外部移除时，释放原预留，仍活动的写入/读取除外。
	for name, e := range s.files {
		if e.pending != nil || e.leases > 0 {
			continue
		}
		if _, err := s.root.Lstat(name); errors.Is(err, os.ErrNotExist) {
			delete(s.files, name)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) deleteLocked(name string) error {
	e := s.files[name]
	info, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		delete(s.files, name)
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || e != nil && e.info != nil && !os.SameFile(e.info, info) {
		return fmt.Errorf("缓存文件身份改变，未删除 %s", name)
	}
	if err = s.remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("清理领取缓存 %s: %w", filepath.Join(s.directory, name), err)
	}
	delete(s.files, name)
	return nil
}

// Close 立即拒绝新领取，通知 HTTP 结束现有请求；最后一个句柄释放后才交还目录独占权。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	close(s.done)
	var errs []error
	for name, e := range s.files {
		if e.pending != nil {
			if err := e.pending.File.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				errs = append(errs, err)
			}
			e.pending.finished = true
			e.pending = nil
		}
		if e.leases == 0 {
			if err := s.deleteLocked(name); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := s.releaseLocked(); err != nil {
		errs = append(errs, err)
	}
	s.closeErr = errors.Join(errs...)
	return s.closeErr
}

func (s *Store) releaseLocked() error {
	if !s.closed || s.leases != 0 || s.unlock == nil {
		return nil
	}
	err := errors.Join(s.root.Close(), s.unlock())
	s.unlock = nil
	return err
}
