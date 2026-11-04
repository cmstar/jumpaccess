// Package uploadcache 管理 HTTP 上传接收资格、暂存内容及任务持有期。
package uploadcache

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/cmstar/jumpaccess/internal/filelock"
)

const (
	RoutePrefix       = "/mcp/uploads/"
	MaxFileSize int64 = 1 << 30
	MaxMetadata       = 1 << 20
)

var (
	ErrTooLarge     = errors.New("上传内容超过限制")
	ErrCapacity     = errors.New("上传暂存额度不足")
	ErrBusy         = errors.New("HTTP 接收并发已满")
	ErrExpired      = errors.New("上传接收对象已过期或不存在")
	ErrInvalidToken = errors.New("上传接收凭据无效")
	ErrClosed       = errors.New("上传接收服务已停止")
	ErrSizeMismatch = errors.New("上传内容长度不符")
	ErrNotReady     = errors.New("上传内容尚未完整接收")
	ErrUsed         = errors.New("上传内容已固定或已提交任务")
	ErrChanged      = errors.New("上传暂存文件身份已变化")
)

type Options struct {
	Directory                      string
	Now                            func() time.Time
	TTL, ScanInterval, LockTimeout time.Duration
	MaxFileSize, MaxBytes          int64
	MaxFiles, MaxTransfers         int
	OnCleanupError                 func(error)
}
type View struct {
	ID             string     `json:"source_id"`
	Size           int64      `json:"size"`
	State          string     `json:"state"`
	ExpiresAt      time.Time  `json:"receive_expires_at"`
	ReadyExpiresAt *time.Time `json:"ready_expires_at,omitempty"`
	UploadID       string     `json:"upload_id,omitempty"`
	Metadata       []byte     `json:"-"`
}
type entry struct {
	view                        View
	info                        os.FileInfo
	receiving, claimed, discard bool
	cancel                      chan struct{}
}
type Store struct {
	mu                sync.Mutex
	options           Options
	root              *os.Root
	secret            [32]byte
	entries           map[string]*entry
	transfers, leases int
	closed            bool
	done              chan struct{}
	unlock            func() error
	remove            func(string) error
	closeErr          error
}

var validID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func filename(id string) string { return ".jumpaccess-upload-" + id + ".part" }

func New(o Options) (*Store, error) {
	if o.Directory == "" {
		return nil, errors.New("上传缓存目录不能为空")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.TTL == 0 {
		o.TTL = 30 * time.Minute
	}
	if o.ScanInterval == 0 {
		o.ScanInterval = time.Minute
	}
	if o.LockTimeout == 0 {
		o.LockTimeout = 200 * time.Millisecond
	}
	if o.MaxFileSize == 0 {
		o.MaxFileSize = MaxFileSize
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 8 << 30
	}
	if o.MaxFiles == 0 {
		o.MaxFiles = 50
	}
	if o.MaxTransfers == 0 {
		o.MaxTransfers = 4
	}
	if o.TTL < 0 || o.ScanInterval < 0 || o.LockTimeout < 0 || o.MaxFileSize < 0 || o.MaxBytes < 0 || o.MaxFiles < 0 || o.MaxTransfers < 0 {
		return nil, errors.New("上传缓存限制必须为正数")
	}
	dir, err := filepath.Abs(o.Directory)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("上传缓存必须是普通目录")
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.LockTimeout)
	defer cancel()
	unlock, err := (filelock.Locker{Dir: dir}).Lock(ctx, "ownership")
	if err != nil {
		return nil, fmt.Errorf("上传缓存正在使用: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		unlock()
		return nil, err
	}
	s := &Store{options: o, root: root, entries: map[string]*entry{}, done: make(chan struct{}), unlock: unlock, remove: root.Remove}
	if _, err = rand.Read(s.secret[:]); err != nil {
		root.Close()
		unlock()
		return nil, err
	}
	// 重启不恢复凭据或任务；只回收可识别的普通文件，失败残留继续占用额度。
	directory, err := root.Open(".")
	if err != nil {
		s.Close()
		return nil, err
	}
	items, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil {
		s.Close()
		return nil, err
	}
	for _, item := range items {
		name := item.Name()
		const prefix = ".jumpaccess-upload-"
		const suffix = ".part"
		if len(name) != len(prefix)+64+len(suffix) || name[:len(prefix)] != prefix || name[len(name)-len(suffix):] != suffix {
			continue
		}
		id := name[len(prefix) : len(prefix)+64]
		if !validID.MatchString(id) {
			continue
		}
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		s.entries[id] = &entry{view: View{ID: id, Size: info.Size(), State: "expired"}, info: info, discard: true}
	}
	s.warn(s.Sweep())
	go func() {
		ticker := time.NewTicker(o.ScanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.warn(s.Sweep())
			}
		}
	}()
	return s, nil
}
func (s *Store) warn(err error) {
	if err != nil && !errors.Is(err, ErrClosed) && s.options.OnCleanupError != nil {
		s.options.OnCleanupError(err)
	}
}
func (s *Store) Done() <-chan struct{} { return s.done }
func (s *Store) expired(e *entry) bool {
	if e.receiving || e.claimed {
		return false
	}
	if e.discard {
		return true
	}
	expires := e.view.ExpiresAt
	if e.view.ReadyExpiresAt != nil {
		expires = *e.view.ReadyExpiresAt
	}
	return !s.options.Now().Before(expires)
}
func (s *Store) sweepLocked() error {
	var errs []error
	for id, e := range s.entries {
		if s.expired(e) {
			e.discard = true
			if err := s.deleteLocked(id); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
func (s *Store) Sweep() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return s.sweepLocked()
}
func (s *Store) removeFileLocked(e *entry) error {
	info, err := s.root.Lstat(filename(e.view.ID))
	if errors.Is(err, os.ErrNotExist) {
		e.info = nil
		return nil
	}
	if err != nil {
		return err
	}
	if e.info == nil || !info.Mode().IsRegular() || !os.SameFile(e.info, info) {
		return ErrChanged
	}
	if err = s.remove(filename(e.view.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	e.info = nil
	return nil
}
func (s *Store) deleteLocked(id string) error {
	e := s.entries[id]
	if e == nil {
		return nil
	}
	if e.receiving || e.claimed {
		return ErrBusy
	}
	if err := s.removeFileLocked(e); err != nil {
		return err
	}
	delete(s.entries, id)
	return nil
}
func (s *Store) Reserve(size int64, metadata []byte) (View, error) {
	s.mu.Lock()
	var clean error
	defer func() { s.mu.Unlock(); s.warn(clean) }()
	if s.closed {
		return View{}, ErrClosed
	}
	if size < 0 || size > s.options.MaxFileSize || len(metadata) > MaxMetadata {
		return View{}, ErrTooLarge
	}
	clean = s.sweepLocked()
	var reserved int64
	for _, e := range s.entries {
		if e.view.Size > s.options.MaxBytes-reserved {
			return View{}, ErrCapacity
		}
		reserved += e.view.Size
	}
	if len(s.entries) >= s.options.MaxFiles || size > s.options.MaxBytes-reserved {
		return View{}, ErrCapacity
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return View{}, err
	}
	id := hex.EncodeToString(random[:])
	e := &entry{view: View{ID: id, Size: size, State: "waiting", ExpiresAt: s.options.Now().Add(s.options.TTL), Metadata: slices.Clone(metadata)}}
	s.entries[id] = e
	return clone(e.view), nil
}
func clone(v View) View {
	v.Metadata = slices.Clone(v.Metadata)
	if v.ReadyExpiresAt != nil {
		t := *v.ReadyExpiresAt
		v.ReadyExpiresAt = &t
	}
	return v
}
func (s *Store) getLocked(id string) (*entry, error) {
	if s.closed {
		return nil, ErrClosed
	}
	e := s.entries[id]
	if e == nil || s.expired(e) || e.discard {
		return nil, ErrExpired
	}
	return e, nil
}
func (s *Store) Get(id string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.getLocked(id)
	if err != nil {
		return View{}, err
	}
	return clone(e.view), nil
}
func (s *Store) signature(id string) string {
	h := hmac.New(sha256.New, s.secret[:])
	h.Write([]byte("upload:" + id))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Store) Signature(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getLocked(id); err != nil {
		return "", err
	}
	return s.signature(id), nil
}

type Receiver struct {
	store    *Store
	e        *entry
	done     <-chan struct{}
	file     *os.File
	written  int64
	finished bool
}

func (s *Store) BeginReceive(id, signature string) (*Receiver, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID.MatchString(id) || !hmac.Equal([]byte(signature), []byte(s.signature(id))) {
		return nil, ErrInvalidToken
	}
	e, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}
	if e.receiving {
		return nil, ErrBusy
	}
	if e.view.State != "waiting" {
		return nil, ErrUsed
	}
	if !s.options.Now().Before(e.view.ExpiresAt) {
		return nil, ErrExpired
	}
	if s.transfers >= s.options.MaxTransfers {
		return nil, ErrBusy
	}
	if e.info != nil {
		if err = s.removeFileLocked(e); err != nil {
			return nil, err
		}
	}
	f, err := s.root.OpenFile(filename(id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	e.info = info
	e.receiving = true
	e.cancel = make(chan struct{})
	e.view.State = "receiving"
	s.transfers++
	s.leases++
	return &Receiver{store: s, e: e, done: e.cancel, file: f}, nil
}
func (r *Receiver) Done() <-chan struct{} { return r.done }
func (r *Receiver) Size() int64           { return r.e.view.Size }
func (r *Receiver) Write(data []byte) (int, error) {
	if r.finished {
		return 0, ErrClosed
	}
	if int64(len(data)) > r.e.view.Size-r.written {
		return 0, ErrSizeMismatch
	}
	n, err := r.file.Write(data)
	r.written += int64(n)
	return n, err
}
func (r *Receiver) Complete() error {
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.finished {
		return ErrUsed
	}
	if s.closed || r.e.discard {
		return ErrClosed
	}
	if r.written != r.e.view.Size {
		return ErrSizeMismatch
	}
	if err := r.file.Sync(); err != nil {
		return err
	}
	if err := r.file.Close(); err != nil {
		return err
	}
	info, err := s.root.Lstat(filename(r.e.view.ID))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, r.e.info) {
		return ErrChanged
	}
	if info.Size() != r.e.view.Size {
		return ErrSizeMismatch
	}
	r.finished = true
	r.e.receiving = false
	r.e.view.State = "ready"
	expires := s.options.Now().Add(s.options.TTL)
	r.e.view.ReadyExpiresAt = &expires
	s.transfers--
	s.leases--
	return nil
}
func (r *Receiver) Abort() error {
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.finished {
		return nil
	}
	r.finished = true
	err := r.file.Close()
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	r.e.receiving = false
	r.e.view.State = "waiting"
	s.transfers--
	s.leases--
	clean := s.removeFileLocked(r.e)
	if s.closed || r.e.discard || s.expired(r.e) {
		r.e.discard = true
		if clean == nil {
			delete(s.entries, r.e.view.ID)
		}
	}
	return errors.Join(err, clean, s.releaseLocked())
}

type Lease struct {
	File  *os.File
	store *Store
	e     *entry
	once  sync.Once
	err   error
}

func (s *Store) Claim(id, task string) (*Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}
	if e.claimed {
		return nil, ErrUsed
	}
	if e.view.State != "ready" {
		return nil, ErrNotReady
	}
	before, err := s.root.Lstat(filename(id))
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || !os.SameFile(before, e.info) {
		return nil, ErrChanged
	}
	f, err := s.root.Open(filename(id))
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(opened, e.info) || opened.Size() != e.view.Size {
		f.Close()
		return nil, ErrChanged
	}
	e.claimed = true
	e.view.State = "submitted"
	e.view.UploadID = task
	s.leases++
	return &Lease{File: f, store: s, e: e}, nil
}
func (l *Lease) Close() error {
	l.once.Do(func() {
		s := l.store
		s.mu.Lock()
		defer s.mu.Unlock()
		err := l.File.Close()
		l.e.claimed = false
		l.e.discard = true
		s.leases--
		l.err = errors.Join(err, s.deleteLocked(l.e.view.ID), s.releaseLocked())
	})
	return l.err
}
func (s *Store) Cancel(id string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.getLocked(id)
	if err != nil {
		return View{}, err
	}
	if e.claimed {
		return View{}, ErrUsed
	}
	e.discard = true
	e.view.State = "cancelled"
	v := clone(e.view)
	if e.receiving {
		close(e.cancel)
		return v, nil
	}
	return v, s.deleteLocked(id)
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	close(s.done)
	var errs []error
	for id, e := range s.entries {
		e.discard = true
		if !e.receiving && !e.claimed {
			if err := s.deleteLocked(id); err != nil {
				errs = append(errs, err)
			}
		}
	}
	errs = append(errs, s.releaseLocked())
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
