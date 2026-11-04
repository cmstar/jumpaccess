package uploadcache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiveRetryClaimAndExpiry(t *testing.T) {
	now := time.Now()
	s, err := New(Options{Directory: t.TempDir(), Now: func() time.Time { return now }, MaxBytes: 8, MaxFiles: 2, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, err := s.Reserve(4, []byte("metadata"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := s.Signature(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginReceive(a.ID, "wrong"); !errors.Is(err, ErrInvalidToken) {
		t.Fatal(err)
	}
	w, err := s.BeginReceive(a.ID, signature)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(a.ID, "early"); !errors.Is(err, ErrNotReady) {
		t.Fatal("partial source committed", err)
	}
	if _, err = s.BeginReceive(a.ID, signature); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent writer accepted", err)
	}
	w.Write([]byte("ab"))
	if err = w.Complete(); !errors.Is(err, ErrSizeMismatch) {
		t.Fatal(err)
	}
	if err = w.Abort(); err != nil {
		t.Fatal(err)
	}
	w, err = s.BeginReceive(a.ID, signature)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("data"))
	now = now.Add(2 * time.Minute)
	if err = w.Complete(); err != nil {
		t.Fatal("active receive expired", err)
	}
	view, _ := s.Get(a.ID)
	if view.State != "ready" || !view.ReadyExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatal(view)
	}
	if _, err = s.BeginReceive(a.ID, signature); err == nil {
		t.Fatal("ready object overwritten")
	}
	lease, err := s.Claim(a.ID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(a.ID, "other"); !errors.Is(err, ErrUsed) {
		t.Fatal("second task claimed source", err)
	}
	now = now.Add(2 * time.Minute)
	if err = s.Sweep(); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err = lease.File.Read(data); err != nil || string(data) != "data" {
		t.Fatal(string(data), err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(a.ID); !errors.Is(err, ErrExpired) {
		t.Fatal("consumed source restored", err)
	}
	b, _ := s.Reserve(8, nil)
	now = now.Add(2 * time.Minute)
	s.Sweep()
	if _, err = s.Get(b.ID); !errors.Is(err, ErrExpired) {
		t.Fatal("unsubmitted source did not expire", err)
	}
	if _, err = s.Reserve(8, nil); err != nil {
		t.Fatal("quota leaked", err)
	}
}

func TestGlobalReceiveLimitAndEmptyFile(t *testing.T) {
	s, err := New(Options{Directory: t.TempDir(), MaxTransfers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Reserve(0, nil)
	b, _ := s.Reserve(0, nil)
	sa, _ := s.Signature(a.ID)
	sb, _ := s.Signature(b.ID)
	w, _ := s.BeginReceive(a.ID, sa)
	if _, err = s.BeginReceive(b.ID, sb); !errors.Is(err, ErrBusy) {
		t.Fatal("global limit ignored", err)
	}
	if err = w.Complete(); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get(a.ID)
	if v.State != "ready" {
		t.Fatal(v)
	}
	w, err = s.BeginReceive(b.ID, sb)
	if err != nil {
		t.Fatal(err)
	}
	w.Abort()
}

func TestCapacityCancellationAndRestartOwnership(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Options{Directory: dir, MaxBytes: 4, MaxFiles: 2, MaxTransfers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Reserve(4, nil)
	sig, _ := s.Signature(a.ID)
	if _, err = s.Reserve(1, nil); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	w, _ := s.BeginReceive(a.ID, sig)
	if _, err = w.Write([]byte("oversized")); !errors.Is(err, ErrSizeMismatch) {
		t.Fatal(err)
	}
	if _, err = s.Cancel(a.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Done():
	default:
		t.Fatal("cancel did not interrupt receiver")
	}
	if err = w.Abort(); err != nil {
		t.Fatal(err)
	}
	a, _ = s.Reserve(4, nil)
	sig, _ = s.Signature(a.ID)
	w, _ = s.BeginReceive(a.ID, sig)
	w.Write([]byte("data"))
	w.Complete()
	lease, _ := s.Claim(a.ID, "task")
	s.Close()
	if next, err := New(Options{Directory: dir}); err == nil {
		next.Close()
		t.Fatal("released ownership while task still reading")
	}
	lease.Close()
	user := filepath.Join(dir, "user.txt")
	os.WriteFile(user, []byte("keep"), 0600)
	orphan := filepath.Join(dir, ".jumpaccess-upload-"+strings.Repeat("a", 64)+".part")
	os.WriteFile(orphan, []byte("old"), 0600)
	next, err := New(Options{Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = next.BeginReceive(a.ID, sig); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("old credential survived restart", err)
	}
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("owned orphan not removed", err)
	}
	if data, _ := os.ReadFile(user); string(data) != "keep" {
		t.Fatal("removed user file")
	}
}

func TestCleanupFailureRetainsQuotaAndIdentity(t *testing.T) {
	now := time.Now()
	s, err := New(Options{Directory: t.TempDir(), Now: func() time.Time { return now }, MaxBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Reserve(4, nil)
	sig, _ := s.Signature(a.ID)
	w, _ := s.BeginReceive(a.ID, sig)
	w.Write([]byte("data"))
	w.Complete()
	remove := s.remove
	s.remove = func(string) error { return os.ErrPermission }
	now = now.Add(time.Hour)
	if err = s.Sweep(); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	if _, err = s.Reserve(1, nil); !errors.Is(err, ErrCapacity) {
		t.Fatal("residual quota released", err)
	}
	s.remove = remove
	s.Sweep()
	if _, err = s.Reserve(4, nil); err != nil {
		t.Fatal(err)
	}
}
