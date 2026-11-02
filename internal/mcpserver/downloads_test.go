package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/downloadcache"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestDownloadLinksUseCurrentCallerAddressWithoutMutatingTask(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	var now atomic.Int64
	now.Store(time.Now().Unix())
	cache, err := downloadcache.New(downloadcache.Options{Directory: t.TempDir(), Now: func() time.Time { return time.Unix(now.Load(), 0) }})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	pending, err := cache.Begin(3, ".gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pending.File.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	artifact, err := pending.Commit("server.log.gz")
	if err != nil {
		t.Fatal(err)
	}
	var engine *agentmcp.Engine
	r := agentmcp.NewRuntime(settings, "gui", func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
		engine = agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID,
			Resolve: func(_ context.Context, target agentmcp.Target) (agentmcp.ResolvedTarget, error) {
				return agentmcp.ResolvedTarget{Target: target}, nil
			},
			Download: func(context.Context, agentmcp.ResolvedTarget, agentmcp.DownloadInput, func(agentmcp.DownloadProgress)) (agentmcp.DownloadResult, error) {
				return agentmcp.DownloadResult{Delivery: "link", Link: &agentmcp.DownloadLink{FileName: "server.log.gz", Format: "gzip", Size: artifact.Size, ExpiresAt: artifact.ExpiresAt, RequiresExtraction: true, RestoreName: "server.log", Artifact: artifact}}, nil
			},
		})
		return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine, Cache: cache})
	})
	defer r.Stop(context.Background())
	status, err := r.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a := sharedOperationClient(t, status.Instance.URL, token, "link-ip")
	endpoint, _ := url.Parse(status.Instance.URL)
	endpoint.Host = strings.Replace(endpoint.Host, "127.0.0.1", "localhost", 1)
	b := sharedOperationClient(t, endpoint.String(), token, "link-localhost")
	info := callOperation[mcpserver.ServerInfo](t, a, "get_server_info", map[string]any{})
	wait := 1000
	in := agentmcp.DownloadInput{Target: agentmcp.Target{Profile: "synthetic", Alias: "log"}, RemotePaths: []string{"/var/log/app.log"}, RequestKey: fmt.Sprintf("%s:%d:00000000-0000-4000-8000-000000000003", status.Instance.ID, info.Data.RequestEpoch), WaitMS: &wait}
	first := callOperation[agentmcp.DownloadView](t, a, "download_files", in)
	if !first.OK || first.Data.Status != "completed" || first.Data.FilesReady || first.Data.Link == nil || !first.Data.Link.Available {
		t.Fatalf("link result=%+v", first)
	}
	second := callOperation[agentmcp.DownloadView](t, b, "get_download", agentmcp.GetDownloadInput{DownloadID: first.Data.ID, WaitMS: &wait})
	if !second.OK || !strings.HasPrefix(first.Data.Link.URL, "http://127.0.0.1:") || !strings.HasPrefix(second.Data.Link.URL, "http://localhost:") {
		t.Fatalf("wrong caller links: first=%s second=%+v", first.Data.Link.URL, second.Data)
	}
	stored, err := engine.GetDownload(context.Background(), agentmcp.GetDownloadInput{DownloadID: first.Data.ID, WaitMS: &wait})
	if err != nil || stored.Link.URL != "" {
		t.Fatalf("query wrote URL back into shared task: %+v %v", stored.Link, err)
	}
	data, _ := json.Marshal(second.Data)
	if strings.Contains(string(data), `"Artifact"`) || strings.Contains(string(data), `"artifact"`) || strings.Contains(string(data), token) {
		t.Fatalf("result leaked internal metadata: %s", data)
	}
	response, err := http.Get(second.Data.Link.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("scoped URL without MCP token=%d", response.StatusCode)
	}
	now.Store(artifact.ExpiresAt.Unix())
	expired := callOperation[agentmcp.DownloadView](t, a, "get_download", agentmcp.GetDownloadInput{DownloadID: first.Data.ID, WaitMS: &wait})
	if !expired.OK || expired.Data.Status != "completed" || expired.Data.Link.Available || expired.Data.Link.URL != "" || expired.Data.Link.UnavailableReason == "" {
		t.Fatalf("expiry changed task or advertised dead link: %+v", expired)
	}
	response, err = http.Get(first.Data.Link.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusGone {
		t.Fatalf("expired HTTP status=%d", response.StatusCode)
	}
}
