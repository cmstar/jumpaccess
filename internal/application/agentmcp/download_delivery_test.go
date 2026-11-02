package agentmcp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
	"github.com/cmstar/jumpaccess/internal/config"
)

func TestDownloadDeliveryPolicy(t *testing.T) {
	for _, tc := range []struct {
		host string
		lan  bool
		want string
	}{
		{"0.0.0.0", false, "local"}, {"localhost", true, "local"},
		{"127.0.0.1", true, "local"}, {"::1", true, "local"},
		{"0.0.0.0", true, "link"}, {"192.168.1.2", true, "link"},
	} {
		if got := DownloadDeliveryMode(config.MCP{ListenHost: tc.host, AllowLAN: tc.lan}); got != tc.want {
			t.Errorf("%s/%v: %s", tc.host, tc.lan, got)
		}
	}
}

func TestReceivePreparedDownloadPermissionFallbackReusesPreparedContent(t *testing.T) {
	for _, scenario := range []string{"open-permission", "write-permission", "remote-permission", "disk-full", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			cache, err := downloadcache.New(downloadcache.Options{Directory: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer cache.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			openLocal := func() (*os.File, error) {
				if scenario == "open-permission" {
					return nil, &os.PathError{Op: "create", Path: "target", Err: os.ErrPermission}
				}
				return os.CreateTemp(t.TempDir(), "local")
			}
			receive := func(file *os.File) error {
				calls++
				if calls == 1 {
					switch scenario {
					case "write-permission":
						return &downloadLocalWriteError{os.ErrPermission}
					case "remote-permission":
						return os.ErrPermission
					case "disk-full":
						return &downloadLocalWriteError{errors.New("disk full")}
					case "cancelled":
						cancel()
						return &downloadLocalWriteError{os.ErrPermission}
					}
				}
				_, err := file.Write([]byte("fixed prepared payload"))
				return err
			}
			a := remoteArtifact{Format: "gzip", Size: 22, SuggestedName: "log.gz"}
			destination, err := receivePreparedDownload(ctx, cache, a, false, openLocal, receive)
			defer destination.Close()
			fallback := scenario == "open-permission" || scenario == "write-permission"
			if fallback {
				if err != nil || !destination.FellBack || destination.Pending == nil {
					t.Fatalf("fallback failed: %+v %v", destination, err)
				}
				if _, err = destination.File.Seek(0, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				content, _ := io.ReadAll(destination.File)
				if string(content) != "fixed prepared payload" {
					t.Fatalf("prepared content changed: %q", content)
				}
				wantCalls := 1
				if scenario == "write-permission" {
					wantCalls = 2
				}
				if calls != wantCalls {
					t.Fatalf("unexpected transfer count: %d", calls)
				}
			} else if err == nil || destination.FellBack || calls != 1 {
				t.Fatalf("invalid retry: %+v %v calls=%d", destination, err, calls)
			}
		})
	}
}

func TestCacheOpenDownloadUsesHandleWithoutReopeningOriginalPath(t *testing.T) {
	cache, err := downloadcache.New(downloadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	file, err := os.CreateTemp(t.TempDir(), "prepared")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.Write([]byte("prepared")); err != nil {
		t.Fatal(err)
	}
	// 路径已经更名，原文件句柄仍可读取；模拟父目录不再允许按原路径打开。
	// Windows 的普通文件句柄不允许更名；仍覆盖从当前非零偏移回到文件开头。
	if runtime.GOOS != "windows" {
		if err = os.Rename(file.Name(), file.Name()+".moved"); err != nil {
			t.Fatal(err)
		}
	}
	a := remoteArtifact{Format: "raw", Size: 8, SuggestedName: "log"}
	artifact, err := cacheOpenDownload(context.Background(), cache, file, a)
	if err != nil || artifact.Size != 8 {
		t.Fatalf("handle import: %+v %v", artifact, err)
	}
}

func TestLinkDownloadLeavesCallerDirectoryOpaqueAndNotReady(t *testing.T) {
	in := DownloadInput{Target: Target{Profile: "test", Alias: "logs"}, RemotePaths: []string{"/log"}, LocalDirectory: "/Users/caller/Downloads", RequestKey: downloadKey("000000000091")}
	e := NewEngine(EngineOptions{Instance: "download-test", DeliveryMode: "link", Resolve: func(_ context.Context, target Target) (ResolvedTarget, error) {
		return ResolvedTarget{Target: target}, nil
	}, Download: func(_ context.Context, _ ResolvedTarget, actual DownloadInput, _ func(DownloadProgress)) (DownloadResult, error) {
		if actual.LocalDirectory != in.LocalDirectory {
			t.Errorf("caller path changed: %s", actual.LocalDirectory)
		}
		return DownloadResult{Delivery: "link", Link: &DownloadLink{FileName: "log.gz"}}, nil
	}})
	defer e.Shutdown(context.Background())
	v, err := e.DownloadFiles(context.Background(), in)
	if err != nil || v.Status != "completed" || v.FilesReady || v.Directory != "" {
		t.Fatalf("%+v %v", v, err)
	}
	v.Link.FileName = "caller mutation"
	copy, err := e.GetDownload(context.Background(), GetDownloadInput{DownloadID: v.ID})
	if err != nil || copy.Link.FileName != "log.gz" {
		t.Fatalf("result alias: %+v %v", copy, err)
	}
}

func TestPreparedLinkDoesNotExtractOrExposeCachePath(t *testing.T) {
	cache, err := downloadcache.New(downloadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	// 链接分支只检查传输长度；不会再读 gzip 内容或在宿主还原。
	file := filepath.Join(t.TempDir(), "artifact")
	if err = os.WriteFile(file, []byte("opaque prepared payload"), 0600); err != nil {
		t.Fatal(err)
	}
	a := remoteArtifact{Format: "gzip", SuggestedName: "log.gz", RootName: "log", Size: 23}
	artifact, err := cache.Import(context.Background(), file, a.Size, ".gz", a.SuggestedName)
	if err != nil {
		t.Fatal(err)
	}
	result := preparedDownloadLink(a, artifact)
	if result.Delivery != "link" || result.Directory != "" || len(result.Files) != 0 || result.Link.FileName != "log.gz" || !result.Link.RequiresExtraction || result.Instructions == "" {
		t.Fatalf("%+v", result)
	}
}

func TestDownloadRenamesExplainConflictingSources(t *testing.T) {
	a := remoteArtifact{Format: "tar.gz", RootName: "result", Wrapped: true, Entries: []remoteEntry{
		{Name: "result", Directory: true},
		{Name: "result/app.log", Source: "/a/app.log"},
		{Name: "result/app.log (2)", Source: "/b/app.log"},
		{Name: "result/logs (2)", Source: "/b/logs", Directory: true},
		{Name: "result/logs (2)/child.log", Source: "/b/logs/child.log"},
	}}
	got := downloadRenames(a)
	if len(got) != 2 || got[0].Source != "/b/app.log" || got[0].ArchivePath != "result/app.log (2)" || got[1].Source != "/b/logs" {
		t.Fatalf("missing source mapping: %+v", got)
	}
	v := DownloadView{DownloadResult: DownloadResult{RenamedEntries: got}}
	copy := cloneDownload(v)
	copy.RenamedEntries[0].Source = "modified"
	if v.RenamedEntries[0].Source != "/b/app.log" {
		t.Fatal("caller changed shared source mapping")
	}
}
