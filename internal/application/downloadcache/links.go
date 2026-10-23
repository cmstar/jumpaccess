package downloadcache

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

var cacheName = regexp.MustCompile(`^\.jumpaccess-download-(exp|pending)-(\d{8}T\d{6}Z)-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(\.[A-Za-z0-9][A-Za-z0-9._-]{0,31})$`)
var suffixPattern = regexp.MustCompile(`^\.[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)

func validSuffix(suffix string) bool {
	return suffixPattern.MatchString(suffix) && !strings.HasSuffix(suffix, ".")
}
func validDownloadName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 1024 || strings.ContainsAny(name, "/\\") {
		return false
	}
	return strings.IndexFunc(name, unicode.IsControl) < 0
}

func parseName(name string) (kind string, timestamp time.Time, ok bool) {
	parts := cacheName.FindStringSubmatch(name)
	if parts == nil || !validSuffix(parts[4]) || parts[1] == "pending" && parts[4] != ".part" {
		return "", time.Time{}, false
	}
	timestamp, err := time.Parse(fileTimeLayout, parts[2])
	if err != nil {
		return "", time.Time{}, false
	}
	return parts[1], timestamp, true
}

func (s *Store) signature(name, downloadName string) string {
	mac := hmac.New(sha256.New, s.secret[:])
	// 只签名链接标识，绝不扫描或散列文件内容。
	_, _ = mac.Write([]byte("jumpaccess-download-v1\x00" + name + "\x00" + downloadName))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Store) Link(artifact Artifact, baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("下载服务地址无效")
	}
	if u.Hostname() == "0.0.0.0" || u.Hostname() == "::" {
		return "", fmt.Errorf("下载服务地址不能使用通配监听地址")
	}
	if !validDownloadName(artifact.DownloadName) {
		return "", ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err = s.readyLocked(artifact.Name); err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + RoutePrefix + artifact.Name
	u.RawPath = ""
	u.RawQuery = url.Values{"filename": {artifact.DownloadName}, "signature": {s.signature(artifact.Name, artifact.DownloadName)}}.Encode()
	return u.String(), nil
}

func (s *Store) readyLocked(name string) (*entry, time.Time, error) {
	if s.closed {
		return nil, time.Time{}, ErrClosed
	}
	kind, expiry, ok := parseName(name)
	if !ok || kind != "exp" {
		return nil, time.Time{}, ErrInvalidName
	}
	if !s.options.Now().Before(expiry) {
		return nil, expiry, ErrExpired
	}
	e := s.files[name]
	if e == nil || e.pending != nil || e.stale {
		return nil, expiry, ErrNotFound
	}
	info, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, expiry, ErrNotFound
	}
	if err != nil {
		return nil, expiry, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(e.info, info) || info.Size() != e.size {
		return nil, expiry, ErrInvalidName
	}
	return e, expiry, nil
}

type Lease struct {
	File         *os.File
	Size         int64
	ExpiresAt    time.Time
	DownloadName string
	store        *Store
	name         string
	head         bool
	once         sync.Once
	closeErr     error
}

func (s *Store) Open(name, downloadName, signature string, head bool) (*Lease, error) {
	kind, _, ok := parseName(name)
	if !ok || kind != "exp" || !validDownloadName(downloadName) {
		return nil, ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if !hmac.Equal([]byte(signature), []byte(s.signature(name, downloadName))) {
		return nil, ErrInvalidToken
	}
	e, expiry, err := s.readyLocked(name)
	if err != nil {
		return nil, err
	}
	if !head && (s.transfers >= s.options.MaxTransfers || e.transfers >= s.options.MaxTransfersPerFile) {
		return nil, ErrBusy
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(e.info, info) || info.Size() != e.size {
		_ = f.Close()
		return nil, ErrInvalidName
	}
	e.leases++
	s.leases++
	if !head {
		e.transfers++
		s.transfers++
	}
	return &Lease{File: f, Size: e.size, ExpiresAt: expiry, DownloadName: downloadName, store: s, name: name, head: head}, nil
}

func (l *Lease) Close() error {
	l.once.Do(func() {
		s := l.store
		s.mu.Lock()
		defer func() {
			s.mu.Unlock()
			s.warn(l.closeErr)
		}()
		var errs []error
		if err := l.File.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			errs = append(errs, err)
		}
		e := s.files[l.name]
		e.leases--
		s.leases--
		if !l.head {
			e.transfers--
			s.transfers--
		}
		if e.leases == 0 && (s.closed || !s.options.Now().Before(l.ExpiresAt)) {
			if err := s.deleteLocked(l.name); err != nil {
				errs = append(errs, err)
			}
		}
		if err := s.releaseLocked(); err != nil {
			errs = append(errs, err)
		}
		l.closeErr = errors.Join(errs...)
	})
	return l.closeErr
}
