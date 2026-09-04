package sftpsession

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockedWriter struct {
	io.WriteCloser
	started chan struct{}
	aborted chan struct{}
	once    sync.Once
}

func (w *blockedWriter) Write([]byte) (int, error) {
	close(w.started)
	<-w.aborted
	return 0, fmt.Errorf("write aborted")
}
func (w *blockedWriter) Abort() error {
	w.once.Do(func() { close(w.aborted) })
	return w.WriteCloser.Close()
}

type blockingRemote struct {
	*localRemote
	writer *blockedWriter
	opens  atomic.Int32
}

func (r *blockingRemote) OpenFile(p string, flags int) (io.WriteCloser, error) {
	f, err := r.localRemote.OpenFile(p, flags)
	if err != nil {
		return nil, err
	}
	if r.opens.Add(1) == 1 {
		r.writer.WriteCloser = f
		return r.writer, nil
	}
	return f, nil
}

func TestCancelBlockedUploadAbortsOnlyFileAndQueueContinues(t *testing.T) {
	r := &blockingRemote{localRemote: newLocalRemote(t), writer: &blockedWriter{started: make(chan struct{}), aborted: make(chan struct{})}}
	t.Cleanup(func() {
		if r.writer.WriteCloser != nil {
			_ = r.writer.Abort()
		}
	})
	m := testManager(t, r)
	s := activeSession(t, m)
	folder := t.TempDir()
	a, b := filepath.Join(folder, "a"), filepath.Join(folder, "b")
	_ = os.WriteFile(a, []byte("one"), 0600)
	_ = os.WriteFile(b, []byte("two"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{a, b}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.writer.started:
	case <-time.After(time.Second):
		t.Fatal("upload did not start")
	}
	if err := m.CancelTransfer(events[0].ID); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "cancelled")
	waitTransfer(t, m, events[1].ID, "completed")
	if _, err := m.ReadDirectory(s.ID, "/"); err != nil {
		t.Fatalf("cancel closed SFTP session: %v", err)
	}
	if _, err := os.Stat(r.local("/a")); !os.IsNotExist(err) {
		t.Fatalf("cancel left partial final file: %v", err)
	}
}

func TestCloseCancelsQueuedTransfersAndReleasesExitProtection(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/a"), []byte("old"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = os.WriteFile(a, []byte("new"), 0600)
	_ = os.WriteFile(b, []byte("two"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{a, b}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	if err := m.Close(s.ID); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		waitTransfer(t, m, e.ID, "cancelled")
	}
	if m.HasActiveTransfers() {
		t.Fatal("closed session still blocks application exit")
	}
}

type standardRenameRemote struct{ *localRemote }

func (r *standardRenameRemote) Rename(a, b string) error {
	if _, err := r.Lstat(b); err == nil {
		return fmt.Errorf("SFTP standard rename destination exists")
	}
	return r.localRemote.Rename(a, b)
}
func (r *standardRenameRemote) PosixRename(a, b string) error { return r.localRemote.Rename(a, b) }

type fallbackRenameRemote struct {
	*standardRenameRemote
	failPublish     bool
	failRestore     bool
	cancelOnPublish func()
}

func (r *fallbackRenameRemote) PosixRename(string, string) error {
	return fmt.Errorf("SFTP destination exists")
}
func (r *fallbackRenameRemote) Rename(a, b string) error {
	if r.failPublish && strings.HasSuffix(a, ".part") {
		if r.cancelOnPublish != nil {
			r.cancelOnPublish()
		}
		return fmt.Errorf("publish rejected")
	}
	if r.failRestore && strings.HasSuffix(a, ".backup") {
		return fmt.Errorf("restore rejected")
	}
	return r.standardRenameRemote.Rename(a, b)
}

func TestOverwriteFallsBackToBackupForServersWithoutAtomicReplacement(t *testing.T) {
	r := &fallbackRenameRemote{standardRenameRemote: &standardRenameRemote{newLocalRemote(t)}}
	_ = os.WriteFile(r.local("/report.txt"), []byte("old"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("new"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	_ = m.ResolveConflict(events[0].ID, "overwrite", false)
	waitTransfer(t, m, events[0].ID, "completed")
	b, _ := os.ReadFile(r.local("/report.txt"))
	if string(b) != "new" {
		t.Fatalf("fallback content=%q", b)
	}
	entries, _ := os.ReadDir(r.root)
	if len(entries) != 1 {
		t.Fatalf("backup or partial file leaked: %v", entries)
	}
}

func TestFailedFallbackPublicationRestoresOriginalFile(t *testing.T) {
	r := &fallbackRenameRemote{standardRenameRemote: &standardRenameRemote{newLocalRemote(t)}, failPublish: true}
	_ = os.WriteFile(r.local("/report.txt"), []byte("original"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("replacement"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	_ = m.ResolveConflict(events[0].ID, "overwrite", false)
	waitTransfer(t, m, events[0].ID, "failed")
	if b, err := os.ReadFile(r.local("/report.txt")); err != nil || string(b) != "original" {
		t.Fatalf("original was not restored: %q, %v", b, err)
	}
}

func TestCancelledReplacementRetainsRecoveryErrorAndBackupPath(t *testing.T) {
	r := &fallbackRenameRemote{standardRenameRemote: &standardRenameRemote{newLocalRemote(t)}, failPublish: true, failRestore: true}
	_ = os.WriteFile(r.local("/report.txt"), []byte("original"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("replacement"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	r.cancelOnPublish = func() { _ = m.CancelTransfer(events[0].ID) }
	_ = m.ResolveConflict(events[0].ID, "overwrite", false)
	failed := waitTransfer(t, m, events[0].ID, "failed")
	if !strings.Contains(failed.Error, ".backup") {
		t.Fatalf("recovery location missing: %+v", failed)
	}
	entries, _ := os.ReadDir(r.root)
	var found bool
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".backup") {
			data, _ := os.ReadFile(filepath.Join(r.root, entry.Name()))
			found = string(data) == "original"
		}
	}
	if !found {
		t.Fatal("original backup was lost")
	}
}

func TestConflictOverwriteReplacesOnlyAfterCompleteWrite(t *testing.T) {
	r := &standardRenameRemote{newLocalRemote(t)}
	_ = os.WriteFile(r.local("/report.txt"), []byte("old"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("replacement"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	if err := m.ResolveConflict(events[0].ID, "overwrite", false); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "completed")
	if b, err := os.ReadFile(r.local("/report.txt")); err != nil || string(b) != "replacement" {
		t.Fatalf("replacement=%q, %v", b, err)
	}
}

func TestCancelConflictUnblocksQueueAndPreservesDestination(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/report.txt"), []byte("old"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("new"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	if !m.HasActiveTransfers() {
		t.Fatal("waiting conflict must block application exit")
	}
	if err := m.CancelTransfer(events[0].ID); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "cancelled")
	if m.HasActiveTransfers() {
		t.Fatal("cancelled transfer still active")
	}
	if b, _ := os.ReadFile(r.local("/report.txt")); string(b) != "old" {
		t.Fatalf("destination changed: %q", b)
	}
}

func TestFailedUploadCanRetryFromStartAfterSourceBecomesAvailable(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "missing.txt")
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitTransfer(t, m, events[0].ID, "failed")
	if failed.Error == "" {
		t.Fatal("failure reason was not retained")
	}
	_ = os.WriteFile(src, []byte("retry"), 0600)
	retry, err := m.RetryTransfer(failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID != failed.ID || retry.Status != "queued" || retry.Transferred != 0 {
		t.Fatalf("retry=%+v", retry)
	}
	done := waitTransfer(t, m, retry.ID, "completed")
	if done.Transferred != 5 {
		t.Fatalf("retry progress=%+v", done)
	}
}

func TestClearCompletedRetainsFailedTasks(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	folder := t.TempDir()
	present := filepath.Join(folder, "present")
	_ = os.WriteFile(present, []byte("data"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{present, filepath.Join(folder, "missing")}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "completed")
	waitTransfer(t, m, events[1].ID, "failed")
	m.ClearCompleted(s.ID)
	remaining := m.ListTransfers(s.ID)
	if len(remaining) != 1 || remaining[0].ID != events[1].ID {
		t.Fatalf("remaining=%+v", remaining)
	}
}

func TestClearCompletedAlsoRemovesCancelledTasks(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/same"), []byte("old"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "same")
	_ = os.WriteFile(src, []byte("new"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	_ = m.CancelTransfer(events[0].ID)
	waitTransfer(t, m, events[0].ID, "cancelled")
	m.ClearCompleted(s.ID)
	if got := m.ListTransfers(s.ID); len(got) != 0 {
		t.Fatalf("cancelled task retained: %+v", got)
	}
}

func TestUploadRecursivelyTransfersDirectoryContents(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "folder")
	_ = os.MkdirAll(filepath.Join(src, "nested"), 0700)
	_ = os.WriteFile(filepath.Join(src, "first.txt"), []byte("one"), 0600)
	_ = os.WriteFile(filepath.Join(src, "nested", "last.txt"), []byte("two"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTransfer(t, m, events[0].ID, "completed")
	if b, err := os.ReadFile(r.local("/folder/nested/last.txt")); err != nil || string(b) != "two" {
		t.Fatalf("nested upload=%q, %v", b, err)
	}
	if done.Transferred != 6 || done.Total != 6 {
		t.Fatalf("directory progress=%+v", done)
	}
}

func TestDownloadStreamsRemoteFileIntoSelectedLocalDirectory(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/remote.txt"), []byte("download"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	dest := t.TempDir()
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/remote.txt"}, Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTransfer(t, m, events[0].ID, "completed")
	if b, err := os.ReadFile(filepath.Join(dest, "remote.txt")); err != nil || string(b) != "download" {
		t.Fatalf("download=%q,%v", b, err)
	}
	if done.Transferred != 8 || done.Total != 8 {
		t.Fatalf("download progress=%+v", done)
	}
}

func TestDownloadRecursivelyTransfersDirectories(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.MkdirAll(r.local("/folder/nested"), 0700)
	_ = os.WriteFile(r.local("/folder/nested/report.txt"), []byte("nested"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	dest := t.TempDir()
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/folder"}, Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "completed")
	if b, err := os.ReadFile(filepath.Join(dest, "folder", "nested", "report.txt")); err != nil || string(b) != "nested" {
		t.Fatalf("nested download=%q,%v", b, err)
	}
}

func TestDownloadSameNamePromptsAndPreservesBothWhenChosen(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/report.txt"), []byte("new"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	dest := t.TempDir()
	_ = os.WriteFile(filepath.Join(dest, "report.txt"), []byte("old"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/report.txt"}, Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	if err := m.ResolveConflict(events[0].ID, "keep-both", false); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "completed")
	if b, err := os.ReadFile(filepath.Join(dest, "report (1).txt")); err != nil || string(b) != "new" {
		t.Fatalf("kept both download=%q,%v", b, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "report.txt")); string(b) != "old" {
		t.Fatalf("original overwritten: %q", b)
	}
}

type beforeReadRemote struct {
	*localRemote
	beforeRead func()
}

func (r *beforeReadRemote) Open(p string) (io.ReadCloser, error) {
	r.beforeRead()
	return r.localRemote.Open(p)
}

func TestDownloadDoesNotOverwriteFileCreatedDuringTransfer(t *testing.T) {
	dest := t.TempDir()
	r := &beforeReadRemote{localRemote: newLocalRemote(t), beforeRead: func() { _ = os.WriteFile(filepath.Join(dest, "report.txt"), []byte("created concurrently"), 0600) }}
	_ = os.WriteFile(r.local("/report.txt"), []byte("remote"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/report.txt"}, Destination: dest})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "failed")
	b, _ := os.ReadFile(filepath.Join(dest, "report.txt"))
	if string(b) != "created concurrently" {
		t.Fatalf("concurrent file overwritten: %q", b)
	}
}

func TestDownloadRequiresExplicitLocalDestination(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	if _, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "download", Sources: []string{"/report.txt"}}); err == nil {
		t.Fatal("empty download destination accepted")
	}
}

func waitTransfer(t *testing.T, m *Manager, id, status string) TransferEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range m.ListTransfers("") {
			if e.ID == id && e.Status == status {
				return e
			}
			if e.ID == id && (e.Status == "failed" || e.Status == "cancelled") && status != e.Status {
				t.Fatalf("transfer ended before %s: %+v", status, e)
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("transfer %q never reached %s: %+v", id, status, m.ListTransfers(""))
	return TransferEvent{}
}
func TestUploadStreamsFileIntoSelectedDirectory(t *testing.T) {
	r := newLocalRemote(t)
	_ = r.Mkdir("/uploads")
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("upload data"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/uploads"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one queued transfer, got %+v", events)
	}
	e := waitTransfer(t, m, events[0].ID, "completed")
	b, err := os.ReadFile(r.local("/uploads/report.txt"))
	if err != nil || string(b) != "upload data" {
		t.Fatalf("uploaded file=%q, %v", b, err)
	}
	if e.Transferred != 11 || e.Total != 11 {
		t.Fatalf("progress=%+v", e)
	}
}

func TestConflictSkipAppliedToBatchPreservesExistingFiles(t *testing.T) {
	r := newLocalRemote(t)
	m := testManager(t, r)
	s := activeSession(t, m)
	folder := t.TempDir()
	sources := []string{filepath.Join(folder, "a.txt"), filepath.Join(folder, "b.txt")}
	for _, src := range sources {
		_ = os.WriteFile(src, []byte("new"), 0600)
		_ = os.WriteFile(r.local("/"+filepath.Base(src)), []byte("old"), 0600)
	}
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: sources, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	if err := m.ResolveConflict(events[0].ID, "skip", true); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		waitTransfer(t, m, e.ID, "skipped")
		b, _ := os.ReadFile(r.local("/" + e.Name))
		if string(b) != "old" {
			t.Fatalf("existing file overwritten: %q", b)
		}
	}
}

func TestConflictKeepBothAllocatesNextAvailableName(t *testing.T) {
	r := newLocalRemote(t)
	_ = os.WriteFile(r.local("/report.txt"), []byte("old"), 0600)
	_ = os.WriteFile(r.local("/report (1).txt"), []byte("first"), 0600)
	m := testManager(t, r)
	s := activeSession(t, m)
	src := filepath.Join(t.TempDir(), "report.txt")
	_ = os.WriteFile(src, []byte("new"), 0600)
	events, err := m.StartTransfer(TransferRequest{SessionID: s.ID, Direction: "upload", Sources: []string{src}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "conflict")
	if err := m.ResolveConflict(events[0].ID, "keep-both", false); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, m, events[0].ID, "completed")
	if b, err := os.ReadFile(r.local("/report (2).txt")); err != nil || string(b) != "new" {
		t.Fatalf("kept both content=%q, %v", b, err)
	}
	if b, _ := os.ReadFile(r.local("/report.txt")); string(b) != "old" {
		t.Fatalf("original changed: %q", b)
	}
}
