package mcpserver_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
)

func TestUploadToolsThroughHTTP(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	var starts atomic.Int32
	var unknown atomic.Bool
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		engine := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, Resolve: func(_ context.Context, target agentmcp.Target) (agentmcp.ResolvedTarget, error) {
			return agentmcp.ResolvedTarget{Target: target}, nil
		}, Upload: func(ctx context.Context, _ agentmcp.ResolvedTarget, _ agentmcp.UploadInput, progress func(agentmcp.UploadProgress)) (agentmcp.UploadResult, error) {
			starts.Add(1)
			if unknown.Load() {
				return agentmcp.UploadResult{}, &agentmcp.Fault{Code: "UPLOAD_RESULT_UNKNOWN", Message: "synthetic unknown publication"}
			}
			progress(agentmcp.UploadProgress{Phase: "transferring", Protocol: "sftp"})
			<-ctx.Done()
			return agentmcp.UploadResult{}, ctx.Err()
		}})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine})
	})
	defer r.Stop(context.Background())
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := sharedOperationClient(t, status.Instance.URL, token, "upload-a")
	b := sharedOperationClient(t, status.Instance.URL, token, "upload-b")
	info := callOperation[mcpserver.ServerInfo](t, a, "get_server_info", map[string]any{})
	zero := 0
	in := agentmcp.UploadInput{Target: agentmcp.Target{Profile: "synthetic", Alias: "target"}, Source: "http", LocalPaths: []string{filepath.Join(t.TempDir(), "file")}, RemoteDirectory: "/target", RequestKey: fmt.Sprintf("%s:%d:00000000-0000-4000-8000-000000000008", status.Instance.ID, info.Data.RequestEpoch), WaitMS: &zero}
	rejected := callOperation[agentmcp.UploadView](t, a, "upload_files", in)
	if rejected.OK || rejected.Error.Code != "LOCAL_SOURCE_REQUIRED" || starts.Load() != 0 || rejected.Error.Details["accepted_this_call"] != false {
		t.Fatalf("nonlocal source: %+v", rejected)
	}
	in.Source = "local"
	first := callOperation[agentmcp.UploadView](t, a, "upload_files", in)
	if !first.OK || first.Data == nil {
		t.Fatalf("start %+v", first)
	}
	wait := 100
	in.WaitMS = &wait
	retry := callOperation[agentmcp.UploadView](t, b, "upload_files", in)
	if !retry.OK || retry.Data.ID != first.Data.ID || starts.Load() != 1 {
		t.Fatalf("retry %+v", retry)
	}
	page := callOperation[agentmcp.Page[agentmcp.UploadView]](t, b, "list_uploads", agentmcp.UploadsPageInput{})
	if !page.OK || len(page.Data.Items) != 1 {
		t.Fatalf("page %+v", page)
	}
	callOperation[agentmcp.UploadView](t, b, "cancel_upload", agentmcp.UploadIDInput{UploadID: first.Data.ID})
	wait = 1000
	final := callOperation[agentmcp.UploadView](t, a, "get_upload", agentmcp.GetUploadInput{UploadID: first.Data.ID, WaitMS: &wait})
	if !final.OK || final.Data.Status != "cancelled" || final.Data.RemoteMayContinue || final.Data.FilesReady {
		t.Fatalf("final %+v", final)
	}
	unknown.Store(true)
	in.RequestKey = fmt.Sprintf("%s:%d:00000000-0000-4000-8000-000000000009", status.Instance.ID, info.Data.RequestEpoch)
	in.WaitMS = &wait
	uncertain := callOperation[agentmcp.UploadView](t, a, "upload_files", in)
	if !uncertain.OK || uncertain.Data.Status != "unknown" || uncertain.Data.RemoteMayContinue || uncertain.Data.FilesReady {
		t.Fatalf("publication unknown: %+v", uncertain)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopped, err := r.Status(context.Background())
	if err != nil || stopped.StopWarning == "" {
		t.Fatalf("missing upload stop warning: %+v %v", stopped, err)
	}
}

func TestHTTPUploadPreparationAndCommitThroughMCP(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	cache, err := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	var starts atomic.Int32
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		e := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, UploadCache: cache, Resolve: func(_ context.Context, target agentmcp.Target) (agentmcp.ResolvedTarget, error) {
			return agentmcp.ResolvedTarget{Target: target, CredentialIdentity: "fixed-login"}, nil
		}, Upload: func(_ context.Context, target agentmcp.ResolvedTarget, _ agentmcp.UploadInput, _ func(agentmcp.UploadProgress)) (agentmcp.UploadResult, error) {
			starts.Add(1)
			if target.CredentialIdentity != "fixed-login" {
				t.Error("identity binding lost")
			}
			return agentmcp.UploadResult{Files: []agentmcp.UploadedFile{{Path: "/target/config", Status: "completed"}}}, nil
		}})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: e, UploadCache: cache})
	})
	defer r.Stop(context.Background())
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := sharedOperationClient(t, status.Instance.URL, token, "http-source-a")
	endpoint, _ := url.Parse(status.Instance.URL)
	endpoint.Host = strings.Replace(endpoint.Host, "127.0.0.1", "localhost", 1)
	b := sharedOperationClient(t, endpoint.String(), token, "http-source-b")
	info := callOperation[mcpserver.ServerInfo](t, a, "get_server_info", map[string]any{})
	key := func(n int) string {
		return fmt.Sprintf("%s:%d:00000000-0000-4000-8000-%012d", status.Instance.ID, info.Data.RequestEpoch, n)
	}
	in := agentmcp.PrepareUploadInput{Target: agentmcp.Target{Profile: "test", Alias: "target"}, RemoteDirectory: "/target", Format: "raw", Size: 4, Entries: []agentmcp.UploadEntry{{Name: "config", Size: 4}}, RequestKey: key(20)}
	first := callOperation[agentmcp.UploadSourceView](t, a, "prepare_upload", in)
	if !first.OK || first.Data.State != "waiting" || first.Data.URL == "" || starts.Load() != 0 {
		t.Fatalf("prepare %+v", first)
	}
	second := callOperation[agentmcp.UploadSourceView](t, b, "prepare_upload", in)
	if !second.OK || second.Data.ID != first.Data.ID || !strings.HasPrefix(second.Data.URL, "http://localhost:") {
		t.Fatalf("prepare retry %+v", second)
	}
	sourceID := first.Data.ID
	pending := callOperation[agentmcp.UploadView](t, a, "commit_upload", agentmcp.CommitUploadInput{SourceID: sourceID, RequestKey: key(21)})
	if pending.OK || pending.Error.Code != "UPLOAD_SOURCE_NOT_READY" || starts.Load() != 0 {
		t.Fatalf("early commit %+v", pending)
	}
	req, _ := http.NewRequest("PUT", second.Data.URL, strings.NewReader("data"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	ready := callOperation[agentmcp.UploadSourceView](t, a, "get_upload_source", agentmcp.UploadSourceInput{SourceID: sourceID})
	if !ready.OK || ready.Data.State != "ready" || ready.Data.URL != "" || starts.Load() != 0 {
		t.Fatalf("HTTP receive mistaken for remote success %+v", ready)
	}
	commit := agentmcp.CommitUploadInput{SourceID: sourceID, RequestKey: key(21)}
	final := callOperation[agentmcp.UploadView](t, a, "commit_upload", commit)
	if !final.OK || final.Data.Status != "completed" || !final.Data.FilesReady || final.Data.Source != "http" {
		t.Fatalf("commit %+v", final)
	}
	retry := callOperation[agentmcp.UploadView](t, b, "commit_upload", commit)
	if !retry.OK || retry.Data.ID != final.Data.ID || starts.Load() != 1 {
		t.Fatalf("commit replayed %+v", retry)
	}
	commit.RequestKey = key(22)
	used := callOperation[agentmcp.UploadView](t, a, "commit_upload", commit)
	if used.OK || starts.Load() != 1 {
		t.Fatalf("consumed source reused %+v", used)
	}
	in.RequestKey = key(23)
	cancelled := callOperation[agentmcp.UploadSourceView](t, a, "prepare_upload", in)
	cancel := callOperation[agentmcp.UploadSourceView](t, b, "cancel_upload_source", agentmcp.UploadSourceInput{SourceID: cancelled.Data.ID})
	if !cancel.OK || cancel.Data.State != "cancelled" {
		t.Fatal(cancel)
	}
	req, _ = http.NewRequest("PUT", cancelled.Data.URL, strings.NewReader("data"))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 410 {
		t.Fatal("cancelled credential accepted")
	}
	logs := r.History().Read(0, 100)
	signature, _ := url.Parse(first.Data.URL)
	for _, line := range logs.Lines {
		if strings.Contains(line.Text, signature.Query().Get("signature")) || strings.Contains(line.Text, first.Data.URL) || strings.Contains(line.Text, token) {
			t.Fatal("log leaked upload credential")
		}
	}
}
