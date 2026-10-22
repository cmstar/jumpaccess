package agentmcp

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadCapabilityCacheScopeBoundAndExpiry(t *testing.T) {
	now := time.Now()
	e := NewEngine(EngineOptions{Instance: "cache", Now: func() time.Time { return now }, MaintenanceInterval: time.Hour})
	defer e.Shutdown(context.Background())
	target := ResolvedTarget{Target: Target{Profile: "p", Organization: "o", AssetID: "a", AccountID: "u"}, Site: "https://example.test", CredentialIdentity: "login-1"}
	e.cacheDownloadCapability(target, downloadCapability{Home: "/home/u", SZ: true, Physical: "/home/u", Virtual: "/"})
	other := target
	other.CredentialIdentity = "login-2"
	if _, ok := e.cachedDownloadCapability(other); ok {
		t.Fatal("shared cache across identities")
	}
	cached, ok := e.cachedDownloadCapability(target)
	if !ok {
		t.Fatal("cache missing")
	}
	now = now.Add(9 * time.Minute)
	e.cacheDownloadCapability(target, cached)
	now = now.Add(2 * time.Minute)
	if _, ok = e.cachedDownloadCapability(target); ok {
		t.Fatal("cache hit extended probe expiry")
	}
	for i := 0; i < 300; i++ {
		target.AssetID = fmt.Sprint(i)
		e.cacheDownloadCapability(target, downloadCapability{Home: "/home/u"})
	}
	if len(e.downloadCapabilities) > 256 {
		t.Fatal("unbounded capabilities")
	}
}

func TestDownloadsReserveCapacityAndReleaseOnStop(t *testing.T) {
	opts := poolOptions(t)
	opts.Download = func(ctx context.Context, _ ResolvedTarget, _ DownloadInput, _ func(DownloadProgress)) (DownloadResult, error) {
		<-ctx.Done()
		return DownloadResult{}, ctx.Err()
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	connection := openTest(t, e, 720, "server")
	zero := 0
	for i := 0; i < 4; i++ {
		_, err := e.DownloadFiles(context.Background(), DownloadInput{ConnectionID: connection.ID, RemotePaths: []string{"/log"}, RequestKey: requestKey(1, 730+i), WaitMS: &zero})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := e.DownloadFiles(context.Background(), DownloadInput{ConnectionID: connection.ID, RemotePaths: []string{"/log"}, RequestKey: requestKey(1, 740), WaitMS: &zero})
	if err == nil {
		t.Fatal("active limit ignored")
	}
	original, _ := e.GetConnection(context.Background(), ConnectionInput{ConnectionID: connection.ID})
	if original.ActiveExecutionID != "" {
		t.Fatal("download occupied command channel")
	}
	if e.Stats().DownloadTransportSlots != 8 {
		t.Fatal("missing transport reservations")
	}
	if err = e.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.Stats().DownloadTransportSlots != 0 || e.Stats().Downloads != 0 {
		t.Fatal("download capacity leaked")
	}
}

func downloadKey(n string) string { return "download-test:1:00000000-0000-0000-0000-" + n }

func TestShutdownKeepsPublishedDownloadWhileCleanupIsUnconfirmed(t *testing.T) {
	published, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	e := NewEngine(EngineOptions{Instance: "download-test", Resolve: func(_ context.Context, target Target) (ResolvedTarget, error) {
		return ResolvedTarget{Target: target}, nil
	}, Download: func(_ context.Context, _ ResolvedTarget, _ DownloadInput, update func(DownloadProgress)) (DownloadResult, error) {
		result := DownloadResult{Directory: "/published", Files: []DownloadedFile{{Path: "/published/log"}}, RemoteArtifacts: []string{"/tmp/.jumpaccess-download-owned.gz"}}
		update(DownloadProgress{Phase: "cleanup", Published: &result})
		close(published)
		<-release
		return result, nil
	}})
	zero := 0
	job, err := e.DownloadFiles(context.Background(), DownloadInput{Target: Target{Profile: "test", Alias: "logs"}, RemotePaths: []string{"/log"}, RequestKey: downloadKey("000000000088"), WaitMS: &zero})
	if err != nil {
		t.Fatal(err)
	}
	<-published
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_ = e.Shutdown(ctx)
	final, err := e.GetDownload(context.Background(), GetDownloadInput{DownloadID: job.ID, WaitMS: &zero})
	if err != nil || !final.FilesReady || final.Status != "completed" || len(final.Warnings) == 0 || len(final.RemoteArtifacts) != 1 {
		t.Fatalf("published result lost %+v %v", final, err)
	}
}
func TestDownloadWaitRetryAndCancellation(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	e := NewEngine(EngineOptions{Instance: "download-test", Resolve: func(_ context.Context, target Target) (ResolvedTarget, error) {
		return ResolvedTarget{Target: target}, nil
	}, Download: func(ctx context.Context, _ ResolvedTarget, _ DownloadInput, update func(DownloadProgress)) (DownloadResult, error) {
		calls.Add(1)
		close(started)
		update(DownloadProgress{Phase: "transferring", Protocol: "sftp"})
		<-ctx.Done()
		return DownloadResult{}, ctx.Err()
	}})
	defer e.Shutdown(context.Background())
	zero := 0
	in := DownloadInput{Target: Target{Profile: "test", Alias: "logs"}, RemotePaths: []string{"/var/log/app.log"}, RequestKey: downloadKey("000000000001"), WaitMS: &zero}
	first, err := e.DownloadFiles(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	queryCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = e.GetDownload(queryCtx, GetDownloadInput{DownloadID: first.ID}); err != nil {
		t.Fatal(err)
	}
	wait := 20
	in.WaitMS = &wait
	second, err := e.DownloadFiles(context.Background(), in)
	if err != nil || first.ID != second.ID || calls.Load() != 1 {
		t.Fatalf("retry: %+v %v calls=%d", second, err, calls.Load())
	}
	if terminal(second.Status) {
		t.Fatalf("query cancellation stopped job: %+v", second)
	}
	in.RemotePaths = []string{"/var/log/other.log"}
	if _, err = e.DownloadFiles(context.Background(), in); err == nil {
		t.Fatal("changed parameters accepted")
	}
	if _, err = e.CancelDownload(context.Background(), DownloadIDInput{DownloadID: first.ID}); err != nil {
		t.Fatal(err)
	}
	wait = 1000
	final, err := e.GetDownload(context.Background(), GetDownloadInput{DownloadID: first.ID, WaitMS: &wait})
	if err != nil || final.Status != "cancelled" || final.FilesReady {
		t.Fatalf("cancel: %+v %v", final, err)
	}
	if e.Stats().Downloads != 0 {
		t.Fatal("active download leaked")
	}
}

func TestDownloadCompletionAndHistoryExpiry(t *testing.T) {
	now := time.Now()
	e := NewEngine(EngineOptions{Instance: "download-test", Now: func() time.Time { return now }, MaintenanceInterval: time.Hour, Limits: Limits{HistoryTTL: time.Second}, Resolve: func(_ context.Context, target Target) (ResolvedTarget, error) {
		return ResolvedTarget{Target: target}, nil
	}, Download: func(context.Context, ResolvedTarget, DownloadInput, func(DownloadProgress)) (DownloadResult, error) {
		return DownloadResult{Directory: "/downloads/unique", Files: []DownloadedFile{{Path: "/downloads/unique/log", Size: 5}}}, nil
	}})
	defer e.Shutdown(context.Background())
	in := DownloadInput{Target: Target{Profile: "test", Alias: "logs"}, RemotePaths: []string{"/log"}, RequestKey: downloadKey("000000000002")}
	final, err := e.DownloadFiles(context.Background(), in)
	if err != nil || final.Status != "completed" || !final.FilesReady {
		t.Fatalf("result %+v %v", final, err)
	}
	final.Files[0].Path = "mutated"
	zero := 0
	again, _ := e.GetDownload(context.Background(), GetDownloadInput{DownloadID: final.ID, WaitMS: &zero})
	if again.Files[0].Path == "mutated" {
		t.Fatal("result aliases stored history")
	}
	page, err := e.DownloadsPage(context.Background(), DownloadsPageInput{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page %+v %v", page, err)
	}
	now = now.Add(2 * time.Second)
	e.maintain()
	if _, err = e.DownloadFiles(context.Background(), in); err == nil {
		t.Fatal("expired result replayed")
	}
}
