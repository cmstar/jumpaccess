package mcpserver_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/agentmcp"
	"github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/ssh"
)

type namedTargetIdentity struct{}

func (namedTargetIdentity) Identity(context.Context, string) (auth.LoginIdentity, error) {
	return auth.LoginIdentity{Site: "https://example.test", LoginID: "synthetic-login"}, nil
}
func (namedTargetIdentity) EnsureFresh(context.Context, string) (credential.Token, error) {
	return credential.Token{Site: "https://example.test", LoginID: "synthetic-login"}, nil
}

type namedTargetResources struct{ lists, gets atomic.Int32 }

func (*namedTargetResources) ListOrganizations(context.Context, string) ([]jumpserver.Organization, error) {
	return []jumpserver.Organization{{ID: "org"}}, nil
}
func (r *namedTargetResources) ListAssets(context.Context, string, string, string, int, int) (jumpserver.AssetPage, error) {
	r.lists.Add(1)
	return jumpserver.AssetPage{Count: 1, Results: []jumpserver.Asset{{ID: "11111111-1111-4111-8111-111111111111", Name: "server"}}}, nil
}
func (r *namedTargetResources) GetAssetByID(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
	r.gets.Add(1)
	return jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "11111111-1111-4111-8111-111111111111"}, Accounts: []jumpserver.Account{{ID: "account", Name: "managed"}}}, nil
}

func TestNamedTargetConnectsThroughHTTPForCLIAndGUI(t *testing.T) {
	for _, host := range []string{"cli", "gui"} {
		t.Run(host, func(t *testing.T) {
			_, settings, token := runtimeFixture(t)
			if err := settings.Store.Update(context.Background(), func(cfg *config.Config) error {
				cfg.CurrentProfile = "work"
				cfg.Profiles["work"] = config.Profile{URL: "https://example.test", Organization: "org"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			resources := &namedTargetResources{}
			dial := sharedOperationSSH(t, func(ch ssh.Channel, requests <-chan *ssh.Request) { defer ch.Close(); ssh.DiscardRequests(requests) })
			r := agentmcp.NewRuntime(settings, host, func(info agentmcp.Instance, cfg config.MCP, token string) http.Handler {
				queries := agentmcp.NewQueries(info.ID, settings.Store, nil, resources)
				connector := agentmcp.Connector{Config: settings.Store, Queries: queries, Auth: namedTargetIdentity{}}
				engine := agentmcp.NewEngine(agentmcp.EngineOptions{Instance: info.ID, Resolve: connector.Resolve, Validate: connector.Validate, Dial: dial})
				return mcpserver.NewHandler(mcpserver.Options{Instance: info, Config: cfg, Token: token, Engine: engine})
			})
			status, err := r.Start(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Stop(context.Background())
			session, err := mcp.NewClient(&mcp.Implementation{Name: "named-target-test", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: status.Instance.URL, HTTPClient: &http.Client{Transport: bearerTransport{token}}, MaxRetries: -1}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			info := callOperation[mcpserver.ServerInfo](t, session, "get_server_info", map[string]any{})
			if !info.OK || info.Data.Host != host {
				t.Fatalf("%+v", info)
			}
			opened := callOperation[agentmcp.OpenResult](t, session, "open_connection", map[string]any{
				"target":      map[string]any{"name": "server"},
				"request_key": info.ServerInstanceID + ":1:00000000-0000-4000-8000-000000000001",
			})
			if !opened.OK || opened.Data == nil || opened.Data.Error != nil || opened.Data.Connection == nil || opened.Data.Status != "completed" {
				t.Fatalf("名称直连未完成：%+v", opened)
			}
			if got := opened.Data.Connection.Target; got.Profile != "work" || got.Organization != "org" || got.AccountID != "account" || got.Name != "" {
				t.Fatalf("返回目标未固定：%+v", got)
			}
			if resources.lists.Load() != 1 || resources.gets.Load() != 1 {
				t.Fatal("重复查询了目标资源")
			}
		})
	}
}
