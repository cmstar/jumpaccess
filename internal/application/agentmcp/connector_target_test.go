package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cmstar/jumpaccess/internal/application/uploadcache"
	"github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

const namedAssetID = "00000000-0000-4000-8000-000000000001"
const otherNamedAssetID = "00000000-0000-4000-8000-000000000002"

func namedConnectorFixture(t *testing.T) (Connector, Settings, *queryResources) {
	t.Helper()
	q, s, resources := queryFixture(t)
	if err := s.Store.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentProfile = "agent"
		p := cfg.Profiles["agent"]
		p.Aliases = map[string]config.Alias{
			"prod":   {Asset: namedAssetID, Organization: "alias-org", Account: "deploy"},
			"single": {Asset: namedAssetID},
		}
		cfg.Profiles["agent"] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	resources.assets = []jumpserver.Asset{{ID: namedAssetID, Name: "server", Address: "192.0.2.1"}}
	resources.accounts = []jumpserver.Account{{ID: "account-a", Name: "deploy", Username: "deploy"}}
	return Connector{Config: s.Store, Queries: q, Auth: &connectorIdentity{login: "one"}}, s, resources
}

func targetJSON(t *testing.T, value string) Target {
	t.Helper()
	var result Target
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestConnectorNamedTargetsUseCurrentProfileAndUniqueAccount(t *testing.T) {
	for _, tc := range []struct{ name, input, organization string }{
		{"alias", `{"name":"prod"}`, "alias-org"},
		{"asset name", `{"name":"server"}`, "configured-org"},
		{"asset address", `{"name":"192.0.2.1"}`, "configured-org"},
		{"asset ID", `{"name":"` + namedAssetID + `"}`, "configured-org"},
		{"explicit profile", `{"profile":"agent","name":"server"}`, "configured-org"},
		{"explicit organization", `{"name":"server","organization":"other-org"}`, "other-org"},
		{"legacy alias", `{"alias":"prod"}`, "alias-org"},
		{"alias without account", `{"name":"single"}`, "configured-org"},
		{"legacy explicit IDs", `{"organization":"org","asset_id":"` + namedAssetID + `","account_id":"account-a"}`, "org"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s, _ := namedConnectorFixture(t)
			before, _ := s.Store.Load()
			got, err := c.Resolve(context.Background(), targetJSON(t, tc.input))
			if err != nil {
				t.Fatal(err)
			}
			want := Target{Profile: "agent", Organization: tc.organization, AssetID: namedAssetID, AccountID: "account-a"}
			if got.Target != want || got.Site != "https://agent.example.test" || got.CredentialIdentity == "" {
				t.Fatalf("resolved target = %+v", got)
			}
			after, _ := s.Store.Load()
			left, _ := json.Marshal(before)
			right, _ := json.Marshal(after)
			if string(left) != string(right) {
				t.Fatal("解析目标修改了用户配置")
			}
		})
	}
}

func TestConnectorNamedTargetAliasPrecedenceAndExplicitAccount(t *testing.T) {
	c, _, resources := namedConnectorFixture(t)
	resources.assets = []jumpserver.Asset{{ID: otherNamedAssetID, Name: "prod"}}
	resources.accounts = append(resources.accounts, jumpserver.Account{ID: "account-b", Name: "other"})
	got, err := c.Resolve(context.Background(), targetJSON(t, `{"name":"prod"}`))
	if err != nil || got.AssetID != namedAssetID || got.AccountID != "account-a" {
		t.Fatalf("未优先使用别名绑定：%+v %v", got, err)
	}
	got, err = c.Resolve(context.Background(), targetJSON(t, `{"name":"prod","organization":"chosen-org","account_id":"account-b"}`))
	if err != nil || got.AssetID != namedAssetID || got.AccountID != "account-b" || got.Organization != "chosen-org" {
		t.Fatalf("显式选择未覆盖别名默认值：%+v %v", got, err)
	}
}

func TestConnectorNamedTargetRejectsAmbiguityAndPreservesFailureCause(t *testing.T) {
	for _, tc := range []struct {
		name, input, code string
		configure         func(*queryResources)
	}{
		{"unknown profile", `{"profile":"missing","name":"server"}`, "PROFILE_NOT_FOUND", nil},
		{"missing name", `{}`, "INVALID_ARGUMENT", nil},
		{"mixed alias", `{"name":"server","alias":"prod"}`, "INVALID_ARGUMENT", nil},
		{"mixed ID", `{"name":"server","asset_id":"` + namedAssetID + `"}`, "INVALID_ARGUMENT", nil},
		{"invalid organization", `{"name":"server","organization":"bad org"}`, "INVALID_ARGUMENT", nil},
		{"asset absent", `{"name":"absent"}`, "TARGET_NOT_FOUND", nil},
		{"network failure", `{"name":"server"}`, "RESOURCE_UNAVAILABLE", func(r *queryResources) { r.fail = errors.New("private upstream details") }},
		{"ambiguous asset", `{"name":"server"}`, "TARGET_AMBIGUOUS", func(r *queryResources) {
			r.assets = append(r.assets, jumpserver.Asset{ID: otherNamedAssetID, Name: "server"})
		}},
		{"ambiguous accounts", `{"name":"server"}`, "TARGET_AMBIGUOUS", func(r *queryResources) {
			r.accounts = append(r.accounts, jumpserver.Account{ID: "account-b", Name: "other"})
		}},
		{"no account", `{"name":"server"}`, "TARGET_INCOMPLETE", func(r *queryResources) { r.accounts = nil }},
		{"interactive account", `{"name":"server"}`, "TARGET_INCOMPLETE", func(r *queryResources) { r.accounts = []jumpserver.Account{{ID: "@INPUT", Name: "manual"}} }},
		{"explicit pseudo account", `{"name":"server","account_id":"@INPUT"}`, "INVALID_ARGUMENT", nil},
		{"unknown explicit account", `{"name":"server","account_id":"missing"}`, "TARGET_NOT_FOUND", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resources := namedConnectorFixture(t)
			if tc.configure != nil {
				tc.configure(resources)
			}
			_, err := c.Resolve(context.Background(), targetJSON(t, tc.input))
			assertFault(t, asFault(err, "INTERNAL_ERROR", "unexpected"), tc.code)
		})
	}
}

func TestConnectorNamedTargetDoesNotFallBackFromMissingCurrentProfile(t *testing.T) {
	c, s, _ := namedConnectorFixture(t)
	if err := s.Store.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentProfile = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := c.Resolve(context.Background(), targetJSON(t, `{"name":"server"}`))
	assertFault(t, err, "PROFILE_REQUIRED")
	got, err := c.Resolve(context.Background(), targetJSON(t, `{"profile":"agent","name":"server"}`))
	if err != nil || got.Profile != "agent" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestConnectorNamedTargetReturnsBoundedResolutionHints(t *testing.T) {
	for _, kind := range []string{"assets", "accounts"} {
		t.Run(kind, func(t *testing.T) {
			c, _, resources := namedConnectorFixture(t)
			if kind == "assets" {
				resources.assets = append(resources.assets, jumpserver.Asset{ID: otherNamedAssetID, Name: "server"})
			} else {
				resources.accounts = append(resources.accounts, jumpserver.Account{ID: "account-b", Username: "other"})
			}
			_, err := c.Resolve(context.Background(), targetJSON(t, `{"name":"server"}`))
			assertFault(t, err, "TARGET_AMBIGUOUS")
			encoded, _ := json.Marshal(err)
			var wire struct {
				Resolution struct {
					Profile, Organization string
					NextTool              string `json:"next_tool"`
					Assets                []AssetView
					Accounts              []AccountView
				}
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Resolution.Profile != "agent" || wire.Resolution.Organization != "configured-org" || wire.Resolution.NextTool != "list_"+kind {
				t.Fatalf("缺少下一步查询范围：%s", encoded)
			}
			if len(wire.Resolution.Assets)+len(wire.Resolution.Accounts) != 2 {
				t.Fatalf("缺少有界候选：%s", encoded)
			}
		})
	}
}

func TestConnectorNamedTargetAccountCandidatesAreBounded(t *testing.T) {
	c, _, resources := namedConnectorFixture(t)
	for i := 0; i < 24; i++ {
		resources.accounts = append(resources.accounts, jumpserver.Account{ID: fmt.Sprintf("account-%d", i)})
	}
	_, err := c.Resolve(context.Background(), targetJSON(t, `{"name":"server"}`))
	var failure *Fault
	if !errors.As(err, &failure) || failure.Resolution == nil || len(failure.Resolution.Accounts) != 20 || !failure.Resolution.Truncated {
		t.Fatalf("账号候选没有按上限截断：%+v", err)
	}
	resources.accounts = []jumpserver.Account{{ID: "account-a"}, {ID: "@INPUT"}}
	resolved, err := c.Resolve(context.Background(), targetJSON(t, `{"name":"server"}`))
	if err != nil || resolved.AccountID != "account-a" {
		t.Fatalf("伪账号干扰唯一托管账号选择：%+v %v", resolved, err)
	}
	resources.accounts = []jumpserver.Account{{ID: "account-a", Name: "deploy"}, {ID: "account-b", Name: "account-a"}}
	resolved, err = c.Resolve(context.Background(), targetJSON(t, `{"name":"server","account_id":"account-a"}`))
	if err != nil || resolved.AccountID != "account-a" {
		t.Fatalf("显式 ID 被当作账号名称：%+v %v", resolved, err)
	}
}

func TestNamedOpenRetryKeepsResolvedDefaults(t *testing.T) {
	c, s, _ := namedConnectorFixture(t)
	opts := poolOptions(t)
	opts.Resolve, opts.Validate = c.Resolve, c.Validate
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	in := OpenInput{Target: targetJSON(t, `{"name":"prod"}`), RequestKey: requestKey(1, 711)}
	first, err := e.Open(context.Background(), in)
	if err != nil || first.Connection == nil || first.Error != nil {
		t.Fatalf("%+v %v", first, err)
	}
	if err := s.Store.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentProfile = "human"
		p := cfg.Profiles["agent"]
		p.Aliases["prod"] = config.Alias{Asset: otherNamedAssetID, Organization: "changed-org", Account: "deploy"}
		cfg.Profiles["agent"] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	retry, err := e.Open(context.Background(), in)
	if err != nil || retry.Connection == nil || retry.Connection.ID != first.Connection.ID || retry.Connection.Target != first.Connection.Target {
		t.Fatalf("重试改变了固定目标：%+v %v", retry, err)
	}
	if err := c.Validate(context.Background(), retry.Connection.Target); err != nil {
		t.Fatal(err)
	}
}

func TestFileTransfersResolveNamedTargetWithoutProfile(t *testing.T) {
	c, _, _ := namedConnectorFixture(t)
	cache, err := uploadcache.New(uploadcache.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	opts := poolOptions(t)
	opts.Resolve, opts.Validate, opts.UploadCache = c.Resolve, c.Validate, cache
	opts.Upload = func(_ context.Context, _ ResolvedTarget, in UploadInput, _ func(UploadProgress)) (UploadResult, error) {
		return UploadResult{Directory: in.RemoteDirectory}, nil
	}
	opts.Download = func(_ context.Context, _ ResolvedTarget, in DownloadInput, _ func(DownloadProgress)) (DownloadResult, error) {
		return DownloadResult{Directory: in.LocalDirectory}, nil
	}
	e := NewEngine(opts)
	defer e.Shutdown(context.Background())
	named := targetJSON(t, `{"name":"prod"}`)
	want := Target{Profile: "agent", Organization: "alias-org", AssetID: namedAssetID, AccountID: "account-a"}
	download, err := e.DownloadFiles(context.Background(), DownloadInput{Target: named, RemotePaths: []string{"/source.txt"}, LocalDirectory: t.TempDir(), RequestKey: requestKey(1, 712)})
	if err != nil || download.Target.Target != want || download.Status != "completed" {
		t.Fatalf("download: %+v %v", download, err)
	}
	upload, err := e.UploadFiles(context.Background(), UploadInput{Target: named, Source: "local", LocalPaths: []string{filepath.Join(t.TempDir(), "source.txt")}, RemoteDirectory: "/target", RequestKey: requestKey(1, 713)})
	if err != nil || upload.Target.Target != want || upload.Status != "completed" {
		t.Fatalf("upload: %+v %v", upload, err)
	}
	source, err := e.PrepareUpload(context.Background(), PrepareUploadInput{Target: named, RemoteDirectory: "/target", Format: "raw", Entries: []UploadEntry{{Name: "empty.txt"}}, RequestKey: requestKey(1, 714)})
	if err != nil || source.Target.Target != want {
		t.Fatalf("HTTP source: %+v %v", source, err)
	}
}
