package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/bootstrap"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResourceToolsThroughHTTPAndMockJumpServer(t *testing.T) {
	_, settings, token := runtimeFixture(t)
	const assetID = "11111111-1111-1111-1111-111111111111"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oauth-fixture" {
			t.Error("没有使用合成 OAuth 凭据")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/users/profile/permissions/":
			_ = json.NewEncoder(w).Encode(map[string]any{"pam_orgs": []jumpserver.Organization{{ID: "org", Name: "合成组织"}}})
		case "/api/v1/perms/users/self/assets/":
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			id := assetID
			if offset > 0 {
				id = "22222222-2222-2222-2222-222222222222"
			}
			_ = json.NewEncoder(w).Encode(jumpserver.AssetPage{Count: 2, Results: []jumpserver.Asset{{ID: id, Name: "fixture", Address: "192.0.2.1"}}})
		case "/api/v1/perms/users/self/assets/" + assetID + "/":
			_ = json.NewEncoder(w).Encode(jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: assetID, Name: "fixture"}, Accounts: []jumpserver.Account{{ID: "account", Name: "managed", Username: "ops"}}, Protocols: []jumpserver.Protocol{{Name: "ssh", Port: 22}}})
		default:
			t.Errorf("非预期资源操作: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	if err := settings.Store.Update(context.Background(), func(c *config.Config) error {
		c.CurrentProfile = "human"
		c.Profiles["human"] = config.Profile{URL: "https://human.example.test"}
		c.Profiles["work"] = config.Profile{URL: upstream.URL, Organization: "org", Aliases: map[string]config.Alias{"server": {Asset: assetID, Account: "account"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	core, err := bootstrap.New(bootstrap.Options{RootDir: filepath.Dir(settings.Store.Path)})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Tokens.Save("work", credential.Token{Site: upstream.URL, AccessToken: "oauth-fixture", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r := core.NewMCP("test")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := r.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop(context.Background())
	session, err := mcp.NewClient(&mcp.Implementation{Name: "resource-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: status.Instance.URL, HTTPClient: &http.Client{Transport: bearerTransport{token}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 7 {
		t.Fatal("查询工具集合不完整", err)
	}
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatal("工具缺少契约或只读标记", tool.Name)
		}
	}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"list_profiles", map[string]any{}}, {"list_organizations", map[string]any{"profile": "work"}},
		{"list_assets", map[string]any{"profile": "work", "organization": "org", "limit": 1}},
		{"get_asset", map[string]any{"profile": "work", "organization": "org", "asset_id": assetID}},
		{"list_accounts", map[string]any{"profile": "work", "organization": "org", "asset_id": assetID}},
		{"list_aliases", map[string]any{"profile": "work"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil || result.IsError || result.StructuredContent == nil {
				t.Fatalf("工具失败: %s %v", tc.name, err)
			}
			data, _ := json.Marshal(result.StructuredContent)
			if strings.Contains(string(data), "oauth-fixture") || strings.Contains(string(data), token) || !strings.Contains(string(data), `"ok":true`) {
				t.Fatal("结果泄露凭据或缺少业务状态")
			}
			if tc.name == "list_assets" {
				var page struct {
					Data struct {
						NextCursor string `json:"next_cursor"`
					}
				}
				if err := json.Unmarshal(data, &page); err != nil || page.Data.NextCursor == "" {
					t.Fatal("没有分页游标", err)
				}
				tc.args["cursor"] = page.Data.NextCursor
				next, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err != nil || next.IsError {
					t.Fatal("后续分页失败", err)
				}
			}
		})
	}
	if err := core.Tokens.Delete("work"); err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_assets", Arguments: map[string]any{"profile": "work", "organization": "org"}})
	if err != nil || !result.IsError {
		t.Fatal("未登录却成功查询", err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(data), "AUTH_REQUIRED") {
		t.Fatal("认证错误没有结构化错误码")
	}
	cfg, _ := settings.Store.Load()
	if cfg.CurrentProfile != "human" {
		t.Fatal("MCP 修改了当前 Profile")
	}
}
