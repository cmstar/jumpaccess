package downloadcache

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T, modify func(*Options)) *Store {
	t.Helper()
	opts := Options{Directory: t.TempDir(), ScanInterval: time.Hour}
	if modify != nil {
		modify(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testArtifact(t *testing.T, s *Store, content string) Artifact {
	t.Helper()
	p, err := s.Begin(int64(len(content)), ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(p.File, content); err != nil {
		t.Fatal(err)
	}
	a, err := p.Commit("server.log.gz")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testOpen(t *testing.T, s *Store, a Artifact, head bool) (*Lease, error) {
	t.Helper()
	link, err := s.Link(a, "http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return s.Open(strings.TrimPrefix(u.Path, RoutePrefix), u.Query().Get("filename"), u.Query().Get("signature"), head)
}

func TestReservationsRejectOversubscriptionAndAbortReleases(t *testing.T) {
	s := testStore(t, func(o *Options) { o.MaxFileSize = 5; o.MaxBytes = 8; o.MaxFiles = 2 })
	p, err := s.Begin(5, ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(4, ".gz"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("want quota rejection: %v", err)
	}
	if _, err := s.Begin(6, ".gz"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want package rejection: %v", err)
	}
	q, err := s.Begin(3, ".tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(0, ".gz"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("want count rejection: %v", err)
	}
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	if err := q.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(5, ".gz"); err != nil {
		t.Fatal(err)
	}
}

func TestCommitChecksLengthAndStartsExpiryWhenReady(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	s := testStore(t, func(o *Options) { o.Now = func() time.Time { return now } })
	p, err := s.Begin(3, ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.File.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Commit("server.log.gz"); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("want size mismatch: %v", err)
	}
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	a := testArtifact(t, s, "abc")
	if want := now.Add(30 * time.Minute); !a.ExpiresAt.Equal(want) {
		t.Fatalf("expiry %s != %s", a.ExpiresAt, want)
	}
	if !strings.Contains(a.Name, "-exp-20261023T084000Z-") {
		t.Fatalf("missing filename expiry: %s", a.Name)
	}
	lease, err := testOpen(t, s, a, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	b, err := io.ReadAll(lease.File)
	if err != nil || string(b) != "abc" {
		t.Fatalf("read %q: %v", b, err)
	}
}

func TestExpiryRejectsNewReadersAndWaitsForLastLease(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	s := testStore(t, func(o *Options) { o.Now = func() time.Time { return now } })
	a := testArtifact(t, s, "abc")
	one, err := testOpen(t, s, a, false)
	if err != nil {
		t.Fatal(err)
	}
	two, err := testOpen(t, s, a, true)
	if err != nil {
		t.Fatal(err)
	}
	link, _ := s.Link(a, "http://localhost:8080")
	u, _ := url.Parse(link)
	now = a.ExpiresAt
	if _, err := s.Open(a.Name, a.DownloadName, u.Query().Get("signature"), false); !errors.Is(err, ErrExpired) {
		t.Fatalf("want expiry: %v", err)
	}
	if err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	if err := one.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.directory, a.Name)); err != nil {
		t.Fatalf("active HEAD must protect: %v", err)
	}
	if err := two.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.directory, a.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired file remains: %v", err)
	}
}

func TestTransferSlotsAndHEAD(t *testing.T) {
	s := testStore(t, nil)
	a := testArtifact(t, s, "abc")
	b := testArtifact(t, s, "def")
	c := testArtifact(t, s, "ghi")
	var leases []*Lease
	t.Cleanup(func() {
		for _, l := range leases {
			_ = l.Close()
		}
	})
	for _, a := range []Artifact{a, a, b, b} {
		l, err := testOpen(t, s, a, false)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, l)
	}
	if _, err := testOpen(t, s, a, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("want per-file limit: %v", err)
	}
	if _, err := testOpen(t, s, c, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("want global limit: %v", err)
	}
	head, err := testOpen(t, s, a, true)
	if err != nil {
		t.Fatalf("HEAD uses no body slot: %v", err)
	}
	_ = head.Close()
	_ = leases[0].Close()
	l, err := testOpen(t, s, c, false)
	if err != nil {
		t.Fatalf("slot not released: %v", err)
	}
	leases = append(leases, l)
}

func TestDeletionFailureKeepsQuotaUntilRetry(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	s := testStore(t, func(o *Options) { o.Now = func() time.Time { return now }; o.MaxBytes = 3 })
	a := testArtifact(t, s, "abc")
	now = a.ExpiresAt
	remove := s.remove
	s.remove = func(string) error { return os.ErrPermission }
	if err := s.Sweep(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("missing cleanup error: %v", err)
	}
	if _, err := s.Begin(1, ".gz"); !errors.Is(err, ErrCapacity) {
		t.Fatalf("deleted quota released early: %v", err)
	}
	s.remove = remove
	if err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Begin(3, ".gz"); err != nil {
		t.Fatal(err)
	}
}

func TestStartupCleansRecognizedFilesOnlyAndLocksOwnership(t *testing.T) {
	dir := t.TempDir()
	ready := ".jumpaccess-download-exp-20991023T083000Z-12345678-1234-4234-8234-123456789012.gz"
	pending := ".jumpaccess-download-pending-20261023T080000Z-12345678-1234-4234-8234-123456789013.part"
	unknown := ".jumpaccess-download-exp-invalid.gz"
	for _, name := range []string{ready, pending, unknown, "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Options{Directory: dir, ScanInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, name := range []string{ready, pending} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old artifact remains: %s %v", name, err)
		}
	}
	for _, name := range []string{unknown, "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("foreign file affected: %s %v", name, err)
		}
	}
	if other, err := New(Options{Directory: dir, LockTimeout: time.Millisecond}); err == nil {
		_ = other.Close()
		t.Fatal("second owner accepted")
	}
}

func TestAuthenticationRejectsChangedFilenameAndRestartedSecret(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Options{Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	a := testArtifact(t, s, "abc")
	link, _ := s.Link(a, "http://localhost:8080")
	u, _ := url.Parse(link)
	sig := u.Query().Get("signature")
	if _, err := s.Open(a.Name, "changed.gz", sig, false); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("filename tamper accepted: %v", err)
	}
	if _, err := s.Open("../"+a.Name, a.DownloadName, sig, false); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("path traversal accepted: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(Options{Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.Open(a.Name, a.DownloadName, sig, false); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old signature accepted: %v", err)
	}
}

func TestCloseKeepsOwnershipUntilActiveReaderFinishes(t *testing.T) {
	s := testStore(t, nil)
	a := testArtifact(t, s, "abc")
	l, err := testOpen(t, s, a, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if other, err := New(Options{Directory: s.directory, LockTimeout: time.Millisecond}); err == nil {
		_ = other.Close()
		t.Fatal("ownership released with active reader")
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(Options{Directory: s.directory})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
}

func TestImportCopiesFixedArtifactAndHonorsCancellation(t *testing.T) {
	s := testStore(t, nil)
	source := filepath.Join(t.TempDir(), "source.gz")
	if err := os.WriteFile(source, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := s.Import(context.Background(), source, 3, ".gz", "日志.gz")
	if err != nil {
		t.Fatal(err)
	}
	if a.DownloadName != "日志.gz" {
		t.Fatal(a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Import(ctx, source, 3, ".gz", "日志.gz"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source removed: %v", err)
	}
}

func TestRepeatedClaimsKeepTheOriginalExpiry(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	s := testStore(t, func(o *Options) { o.Now = func() time.Time { return now } })
	a := testArtifact(t, s, "abc")
	now = now.Add(20 * time.Minute)
	for _, head := range []bool{true, false, false} {
		lease, err := testOpen(t, s, a, head)
		if err != nil {
			t.Fatal(err)
		}
		if !lease.ExpiresAt.Equal(a.ExpiresAt) {
			t.Fatalf("claim extended expiry: %s", lease.ExpiresAt)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}
	now = a.ExpiresAt
	if _, err := s.Link(a, "http://localhost:8080"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired result acquired new link: %v", err)
	}
}

func TestSymlinksAreNeitherClaimedNorCleaned(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(target, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	name := ".jumpaccess-download-exp-20991023T083000Z-12345678-1234-4234-8234-123456789012.gz"
	link := filepath.Join(dir, name)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("当前系统不允许创建符号链接: %v", err)
	}
	s, err := New(Options{Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("startup removed symlink: %v", err)
	}
	if _, err := s.Open(name, "keep.gz", s.signature(name, "keep.gz"), false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unowned symlink claim: %v", err)
	}
	a := testArtifact(t, s, "abc")
	if err := os.Remove(filepath.Join(dir, a.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, a.Name)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(a.Name, a.DownloadName, s.signature(a.Name, a.DownloadName), false); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("replacement symlink claim: %v", err)
	}
	_ = s.Close()
	if _, err := os.Lstat(filepath.Join(dir, a.Name)); err != nil {
		t.Fatalf("shutdown removed symlink: %v", err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "abc" {
		t.Fatalf("symlink target changed: %q %v", b, err)
	}
}

func TestConcurrentReservationsNeverExceedPackageCount(t *testing.T) {
	s := testStore(t, func(o *Options) { o.MaxFiles = 3 })
	var wg sync.WaitGroup
	var mu sync.Mutex
	var pending []*Pending
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := s.Begin(1, ".gz")
			if err != nil {
				if !errors.Is(err, ErrCapacity) {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			mu.Lock()
			pending = append(pending, p)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(pending) != 3 {
		t.Fatalf("reserved %d files, want 3", len(pending))
	}
	for _, p := range pending {
		if err := p.Abort(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimerRemovesExpiredReadyFiles(t *testing.T) {
	s := testStore(t, func(o *Options) {
		o.Now = func() time.Time { return time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC) }
		o.ScanInterval = 5 * time.Millisecond
	})
	a := testArtifact(t, s, "abc")
	s.mu.Lock()
	s.options.Now = func() time.Time { return a.ExpiresAt }
	s.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for {
		_, err := os.Stat(filepath.Join(s.directory, a.Name))
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timer did not reclaim file: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStoppingCancelsNewWorkAndAbortsPending(t *testing.T) {
	s := testStore(t, nil)
	p, err := s.Begin(3, ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(p.File, "ab"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("shutdown signal not closed")
	}
	if _, err := s.Begin(1, ".gz"); !errors.Is(err, ErrClosed) {
		t.Fatalf("stopped store accepted work: %v", err)
	}
	if _, err := p.Commit("file.gz"); !errors.Is(err, ErrClosed) {
		t.Fatalf("stopped store committed: %v", err)
	}
	if err := p.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.directory, p.name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending remained: %v", err)
	}
}

func TestCommitAcceptsPreSizedSparseFile(t *testing.T) {
	s := testStore(t, nil)
	p, err := s.Begin(1000, ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.File.Truncate(1000); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Commit("large.gz"); err != nil {
		t.Fatal(err)
	}
}

func TestLastExpiredLeaseReportsCleanupFailure(t *testing.T) {
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	warnings := make(chan error, 1)
	s := testStore(t, func(o *Options) {
		o.Now = func() time.Time { return now }
		o.OnCleanupError = func(err error) { warnings <- err }
	})
	a := testArtifact(t, s, "abc")
	l, err := testOpen(t, s, a, false)
	if err != nil {
		t.Fatal(err)
	}
	now = a.ExpiresAt
	remove := s.remove
	s.remove = func(string) error { return os.ErrPermission }
	if err := l.Close(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("missing close cleanup error: %v", err)
	}
	select {
	case err := <-warnings:
		if !errors.Is(err, os.ErrPermission) {
			t.Fatal(err)
		}
	default:
		t.Fatal("last HTTP lease cleanup failure did not reach warning callback")
	}
	s.remove = remove
	if err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.directory, a.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("later sweep failed to remove artifact: %v", err)
	}
}
