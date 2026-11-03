package agentmcp

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestUploadTaskRetryCancelAndHistory(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	opts := poolOptions(t)
	opts.Upload = func(ctx context.Context, _ ResolvedTarget, _ UploadInput, update func(UploadProgress)) (UploadResult, error) {
		calls.Add(1)
		close(started)
		update(UploadProgress{Phase: "transferring", Protocol: "sftp"})
		<-ctx.Done()
		return UploadResult{}, ctx.Err()
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	zero := 0
	in := UploadInput{Target: Target{Profile: "p", Alias: "test"}, Source: "local", LocalPaths: []string{filepath.Join(t.TempDir(), "file")}, RemoteDirectory: "/target", RequestKey: requestKey(1, 710), WaitMS: &zero}
	first, err := e.UploadFiles(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	second, err := e.UploadFiles(context.Background(), in)
	if err != nil || second.ID != first.ID || calls.Load() != 1 {
		t.Fatalf("retry: %+v %v", second, err)
	}
	if e.Stats().Uploads != 1 || e.Stats().UploadTransportSlots != 2 {
		t.Fatal("missing upload stats")
	}
	page, err := e.UploadsPage(context.Background(), UploadsPageInput{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	if _, err = e.CancelUpload(context.Background(), UploadIDInput{UploadID: first.ID}); err != nil {
		t.Fatal(err)
	}
	wait := 2000
	done, err := e.GetUpload(context.Background(), GetUploadInput{UploadID: first.ID, WaitMS: &wait})
	if err != nil || done.Status != "cancelled" || done.FilesReady || e.Stats().Uploads != 0 || e.Stats().UploadTransportSlots != 0 {
		t.Fatalf("cancel: %+v %v", done, err)
	}
	e.mu.Lock()
	e.history.removeLocked(e.history.records[first.ID])
	e.mu.Unlock()
	if _, err = e.UploadFiles(context.Background(), in); err == nil || calls.Load() != 1 {
		t.Fatal("expired upload replayed")
	}
}

func TestUploadRequiresExplicitLocalSource(t *testing.T) {
	e := NewEngine(poolOptions(t))
	defer e.Shutdown(context.Background())
	for _, source := range []string{"", "http", "remote"} {
		_, err := e.UploadFiles(context.Background(), UploadInput{Target: Target{Profile: "p"}, Source: source, LocalPaths: []string{filepath.Join(t.TempDir(), "file")}, RemoteDirectory: "/target", RequestKey: requestKey(1, 711)})
		if err == nil {
			t.Fatalf("source %q accepted", source)
		}
	}
}

func TestUploadsReserveCapacityAndReleaseOnStop(t *testing.T) {
	opts := poolOptions(t)
	opts.Upload = func(ctx context.Context, _ ResolvedTarget, _ UploadInput, _ func(UploadProgress)) (UploadResult, error) {
		<-ctx.Done()
		return UploadResult{}, ctx.Err()
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	connection := openTest(t, e, 750, "server")
	zero := 0
	in := UploadInput{ConnectionID: connection.ID, Source: "local", LocalPaths: []string{filepath.Join(t.TempDir(), "file")}, RemoteDirectory: "/target", WaitMS: &zero}
	for i := 0; i < 4; i++ {
		in.RequestKey = requestKey(1, 751+i)
		if _, err := e.UploadFiles(context.Background(), in); err != nil {
			t.Fatal(err)
		}
	}
	in.RequestKey = requestKey(1, 755)
	if _, err := e.UploadFiles(context.Background(), in); err == nil {
		t.Fatal("active upload limit ignored")
	}
	bound, err := e.GetConnection(context.Background(), ConnectionInput{ConnectionID: connection.ID})
	if err != nil || bound.ActiveExecutionID != "" || e.Stats().UploadTransportSlots != 8 {
		t.Fatalf("upload occupied command channel or lost reservations: %+v %v", bound, err)
	}
	if err = e.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.Stats().Uploads != 0 || e.Stats().UploadTransportSlots != 0 {
		t.Fatal("upload capacity leaked")
	}
}

func TestUploadShutdownPreservesConfirmedPublication(t *testing.T) {
	published, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	opts := poolOptions(t)
	opts.Upload = func(_ context.Context, _ ResolvedTarget, _ UploadInput, update func(UploadProgress)) (UploadResult, error) {
		result := UploadResult{Directory: "/target", Files: []UploadedFile{{Path: "/target/file", Status: "completed"}}}
		update(UploadProgress{Phase: "cleanup", Published: &result})
		close(published)
		<-release
		return result, nil
	}
	e := NewEngine(opts)
	zero := 0
	job, err := e.UploadFiles(context.Background(), UploadInput{Target: Target{Profile: "p", Alias: "test"}, Source: "local", LocalPaths: []string{filepath.Join(t.TempDir(), "file")}, RemoteDirectory: "/target", RequestKey: requestKey(1, 712), WaitMS: &zero})
	if err != nil {
		t.Fatal(err)
	}
	<-published
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*20)
	defer cancel()
	_ = e.Shutdown(ctx)
	final, err := e.GetUpload(context.Background(), GetUploadInput{UploadID: job.ID, WaitMS: &zero})
	if err != nil || final.Status != "completed" || !final.FilesReady || len(final.Warnings) == 0 {
		t.Fatalf("shutdown: %+v %v", final, err)
	}
}
