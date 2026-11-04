package agentmcp

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
)

func TestUploadSourcePrepareCommitAndRetry(t *testing.T) {
	cache, err := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	var resolves, starts atomic.Int32
	opts := poolOptions(t)
	opts.UploadCache = cache
	resolve := opts.Resolve
	opts.Resolve = func(ctx context.Context, in Target) (ResolvedTarget, error) { resolves.Add(1); return resolve(ctx, in) }
	opts.Upload = func(_ context.Context, _ ResolvedTarget, in UploadInput, _ func(UploadProgress)) (UploadResult, error) {
		starts.Add(1)
		if in.prepared == nil {
			t.Error("missing staged source")
		}
		data, _ := io.ReadAll(in.prepared.File)
		if string(data) != "data" {
			t.Error("wrong content")
		}
		return UploadResult{Directory: in.RemoteDirectory, Files: []UploadedFile{{Name: "file.zip", Path: "/target/file.zip", Status: "completed"}}}, nil
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	in := PrepareUploadInput{Target: Target{Profile: "p", Alias: "test"}, RemoteDirectory: "/target", Format: "raw", Size: 4, Entries: []UploadEntry{{Name: "file.zip", Size: 4}}, RequestKey: requestKey(1, 800)}
	source, err := e.PrepareUpload(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.PrepareUpload(context.Background(), in)
	if err != nil || again.ID != source.ID || resolves.Load() != 1 {
		t.Fatalf("prepare replayed %+v %v", again, err)
	}
	commit := CommitUploadInput{SourceID: source.ID, RequestKey: requestKey(1, 801)}
	if _, err = e.CommitUpload(context.Background(), commit); err == nil {
		t.Fatal("partial source accepted")
	}
	sig, _ := cache.Signature(source.ID)
	receiver, _ := cache.BeginReceive(source.ID, sig)
	receiver.Write([]byte("data"))
	receiver.Complete()
	job, err := e.CommitUpload(context.Background(), commit)
	if err != nil || job.Status != "completed" || !job.FilesReady {
		t.Fatalf("commit %+v %v", job, err)
	}
	retry, err := e.CommitUpload(context.Background(), commit)
	if err != nil || retry.ID != job.ID || starts.Load() != 1 {
		t.Fatalf("commit replayed %+v %v", retry, err)
	}
	commit.RequestKey = requestKey(1, 802)
	if _, err = e.CommitUpload(context.Background(), commit); err == nil || starts.Load() != 1 {
		t.Fatal("second task consumed same source")
	}
	if _, err = e.PrepareUpload(context.Background(), in); err == nil {
		t.Fatal("expired source recreated")
	}
}

func TestUploadSourceManifestAndIdentity(t *testing.T) {
	cache, _ := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
	defer cache.Close()
	opts := poolOptions(t)
	opts.UploadCache = cache
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	for i, entries := range [][]UploadEntry{{{Name: "../outside"}}, {{Name: "dir/child"}}, {{Name: "same"}, {Name: "SAME"}}, {{Name: "dir", Directory: true, Size: 1}}} {
		_, err := e.PrepareUpload(context.Background(), PrepareUploadInput{Target: Target{Profile: "p", Alias: "test"}, RemoteDirectory: "/target", Format: "tar.gz", Size: 4, Entries: entries, RequestKey: requestKey(1, 810+i)})
		if err == nil {
			t.Fatalf("unsafe manifest accepted: %+v", entries)
		}
	}
	source, err := e.PrepareUpload(context.Background(), PrepareUploadInput{Target: Target{Profile: "p", Alias: "test"}, RemoteDirectory: "/target", Format: "raw", Size: 0, Entries: []UploadEntry{{Name: "empty"}}, RequestKey: requestKey(1, 815)})
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := cache.Signature(source.ID)
	r, _ := cache.BeginReceive(source.ID, sig)
	r.Complete()
	e.opts.Validate = func(context.Context, ResolvedTarget) error {
		return fault("IDENTITY_CHANGED", "synthetic changed identity")
	}
	e.opts.Upload = func(context.Context, ResolvedTarget, UploadInput, func(UploadProgress)) (UploadResult, error) {
		t.Error("transferred under changed identity")
		return UploadResult{}, nil
	}
	job, err := e.CommitUpload(context.Background(), CommitUploadInput{SourceID: source.ID, RequestKey: requestKey(1, 816), TimeoutMS: int(time.Second / time.Millisecond)})
	if err != nil || job.Error == nil || job.Error.Code != "IDENTITY_CHANGED" {
		t.Fatalf("identity result %+v %v", job, err)
	}
	if _, err = cache.Get(source.ID); err == nil {
		t.Fatal("failed task retained claimed cache")
	}
}
