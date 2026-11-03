package mcpserver_test

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
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
