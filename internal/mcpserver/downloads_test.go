package mcpserver_test

import (
	"context"
	"fmt"
	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestDownloadToolsThroughHTTP(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	var starts atomic.Int32
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		engine := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, Resolve: func(_ context.Context, target agentmcp.Target) (agentmcp.ResolvedTarget, error) {
			return agentmcp.ResolvedTarget{Target: target}, nil
		}, Download: func(ctx context.Context, _ agentmcp.ResolvedTarget, _ agentmcp.DownloadInput, progress func(agentmcp.DownloadProgress)) (agentmcp.DownloadResult, error) {
			starts.Add(1)
			progress(agentmcp.DownloadProgress{Phase: "transferring", Protocol: "sftp"})
			<-ctx.Done()
			return agentmcp.DownloadResult{}, ctx.Err()
		}})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine})
	})
	defer r.Stop(context.Background())
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := sharedOperationClient(t, status.Instance.URL, token, "download-a")
	b := sharedOperationClient(t, status.Instance.URL, token, "download-b")
	info := callOperation[mcpserver.ServerInfo](t, a, "get_server_info", map[string]any{})
	zero := 0
	in := agentmcp.DownloadInput{Target: agentmcp.Target{Profile: "synthetic", Alias: "log"}, RemotePaths: []string{"/var/log/app.log"}, RequestKey: fmt.Sprintf("%s:%d:00000000-0000-4000-8000-000000000001", status.Instance.ID, info.Data.RequestEpoch), WaitMS: &zero}
	first := callOperation[agentmcp.DownloadView](t, a, "download_files", in)
	if !first.OK || first.Data == nil {
		t.Fatalf("start %+v", first)
	}
	wait := 100
	in.WaitMS = &wait
	retry := callOperation[agentmcp.DownloadView](t, b, "download_files", in)
	if !retry.OK || retry.Data.ID != first.Data.ID || starts.Load() != 1 {
		t.Fatalf("retry %+v", retry)
	}
	query := callOperation[agentmcp.DownloadView](t, b, "get_download", agentmcp.GetDownloadInput{DownloadID: first.Data.ID, WaitMS: &zero})
	if !query.OK || query.Data.FilesReady {
		t.Fatalf("query %+v", query)
	}
	page := callOperation[agentmcp.Page[agentmcp.DownloadView]](t, b, "list_downloads", agentmcp.DownloadsPageInput{})
	if !page.OK || len(page.Data.Items) != 1 {
		t.Fatalf("list %+v", page)
	}
	callOperation[agentmcp.DownloadView](t, b, "cancel_download", agentmcp.DownloadIDInput{DownloadID: first.Data.ID})
	wait = 1000
	final := callOperation[agentmcp.DownloadView](t, a, "get_download", agentmcp.GetDownloadInput{DownloadID: first.Data.ID, WaitMS: &wait})
	if !final.OK || final.Data.Status != "cancelled" || final.Data.RemoteMayContinue {
		t.Fatalf("final %+v", final)
	}
}
