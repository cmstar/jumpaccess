package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type aliasListResources struct {
	fakeResourceService
	organizations func(context.Context, string) ([]jumpserver.Organization, error)
	find          func(context.Context, string, string, string) (jumpserver.AssetDetail, error)
}

func (f aliasListResources) ListOrganizations(ctx context.Context, profile string) ([]jumpserver.Organization, error) {
	return f.organizations(ctx, profile)
}

func (f aliasListResources) FindAsset(ctx context.Context, profile, organization, reference string) (jumpserver.AssetDetail, error) {
	return f.find(ctx, profile, organization, reference)
}

func aliasListStore(t *testing.T, aliases map[string]projectconfig.Alias) projectconfig.Store {
	t.Helper()
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	value := projectconfig.Default()
	value.CurrentProfile = "other"
	value.Profiles["other"] = projectconfig.Profile{URL: "https://other.example.test"}
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test", Organization: "org-prod", Aliases: aliases}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAliasListResolvesNamesWithinProfileAndOrganization(t *testing.T) {
	for _, long := range []bool{false, true} {
		t.Run(map[bool]string{false: "table", true: "long"}[long], func(t *testing.T) {
			store := aliasListStore(t, map[string]projectconfig.Alias{
				"order":      {Asset: "asset-1", Account: "DEPLOY"},
				"order-copy": {Asset: "asset-1"},
				"test":       {Asset: "asset-1", Account: "account-1", Organization: "org-test"},
			})
			before, _ := store.Load()
			orgCalls := 0
			assetCalls := map[string]int{}
			service := aliasListResources{
				organizations: func(_ context.Context, profile string) ([]jumpserver.Organization, error) {
					orgCalls++
					if profile != "work" {
						t.Fatalf("profile = %s", profile)
					}
					return []jumpserver.Organization{{ID: "org-prod", Name: "生产环境"}, {ID: "org-test", Name: "测试环境"}}, nil
				},
				find: func(_ context.Context, profile, org, ref string) (jumpserver.AssetDetail, error) {
					if profile != "work" || ref != "asset-1" {
						t.Fatalf("unexpected query: %s %s %s", profile, org, ref)
					}
					assetCalls[org]++
					name := "订单服务"
					if org == "org-test" {
						name = "测试服务"
					}
					return jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1", Name: name, Address: "10.20.1.11"}, Accounts: []jumpserver.Account{{ID: "account-1", Name: "应用部署账号", Username: "deploy"}}}, nil
				},
			}
			var stdout bytes.Buffer
			root := NewRoot(Dependencies{Store: store, Resources: service, Stdout: &stdout})
			args := []string{"alias", "list", "--profile", "work"}
			if long {
				args = append(args, "--long")
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"订单服务", "测试服务", "10.20.1.11", "应用部署账号（deploy）", "生产环境（继承）", "测试环境", "未绑定"} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("missing %q in %s", want, stdout.String())
				}
			}
			if orgCalls != 1 || assetCalls["org-prod"] != 1 || assetCalls["org-test"] != 1 {
				t.Fatalf("queries: %d %v", orgCalls, assetCalls)
			}
			if long {
				for _, want := range []string{"ASSET REF", "ACCOUNT REF", "ORG REF", "asset-1", "DEPLOY", "org-prod", "org-test"} {
					if !strings.Contains(stdout.String(), want) {
						t.Errorf("long output missing %q", want)
					}
				}
				if strings.Count(stdout.String(), "\n\n") != 2 {
					t.Errorf("long entries must be separated: %q", stdout.String())
				}
			} else if strings.Contains(stdout.String(), "asset-1") || strings.Contains(stdout.String(), "account-1") || strings.Contains(stdout.String(), "org-prod") {
				t.Errorf("resolved table contains raw IDs: %s", stdout.String())
			}
			after, _ := store.Load()
			if after.Profiles["work"].Aliases["order"] != before.Profiles["work"].Aliases["order"] {
				t.Fatal("listing rewrote alias references")
			}
		})
	}
}

func TestAliasListLookupFailuresPreserveReferences(t *testing.T) {
	store := aliasListStore(t, map[string]projectconfig.Alias{"order": {Asset: "asset-1", Account: "account-1"}, "copy": {Asset: "asset-1"}})
	for _, long := range []bool{false, true} {
		calls := 0
		service := aliasListResources{
			organizations: func(context.Context, string) ([]jumpserver.Organization, error) {
				return nil, errors.New("secret response")
			},
			find: func(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
				calls++
				return jumpserver.AssetDetail{}, errors.New("secret response")
			},
		}
		var stdout, stderr bytes.Buffer
		root := NewRoot(Dependencies{Store: store, Resources: service, Stdout: &stdout, Stderr: &stderr})
		args := []string{"alias", "list", "--profile", "work"}
		if long {
			args = append(args, "--long")
		}
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"asset-1（名称暂不可用）", "account-1（名称暂不可用）", "org-prod（名称暂不可用）（继承）", "未绑定"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("missing %q in %s", want, stdout.String())
			}
		}
		if calls != 1 {
			t.Errorf("failed lookup was repeated %d times", calls)
		}
		if strings.Contains(stdout.String()+stderr.String(), "secret response") {
			t.Fatal("leaked lookup error")
		}
	}
}

func TestAliasListEmptyDoesNotQueryResources(t *testing.T) {
	store := aliasListStore(t, nil)
	service := aliasListResources{
		organizations: func(context.Context, string) ([]jumpserver.Organization, error) {
			t.Fatal("unexpected lookup")
			return nil, nil
		},
		find: func(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
			t.Fatal("unexpected lookup")
			return jumpserver.AssetDetail{}, nil
		},
	}
	for _, long := range []bool{false, true} {
		var stdout bytes.Buffer
		root := NewRoot(Dependencies{Store: store, Resources: service, Stdout: &stdout})
		args := []string{"alias", "list", "--profile", "work"}
		if long {
			args = append(args, "--long")
		}
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !long && stdout.String() != "ALIAS  ASSET  ADDRESS  ACCOUNT  ORGANIZATION\n" {
			t.Fatal(stdout.String())
		}
		if long && stdout.String() != "暂无 Alias\n" {
			t.Fatal(stdout.String())
		}
	}
}

func TestResourceListShowsMissingNames(t *testing.T) {
	service := fakeResourceService{
		organizations: []jumpserver.Organization{{ID: "org-1"}},
		assets:        jumpserver.AssetPage{Results: []jumpserver.Asset{{ID: "asset-1", Address: "10.0.0.1"}}},
		asset:         jumpserver.AssetDetail{Accounts: []jumpserver.Account{{ID: "account-1", Username: "root"}}},
	}
	for _, args := range [][]string{{"organization", "list"}, {"asset", "list"}, {"account", "list", "asset-1"}} {
		var stdout bytes.Buffer
		root := NewRoot(Dependencies{Resources: service, Stdout: &stdout})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "名称未提供") {
			t.Fatal(stdout.String())
		}
	}
}

func TestTableAlignsChineseAndCombiningCharacters(t *testing.T) {
	var stdout bytes.Buffer
	if err := writeTable(&stdout, []string{"NAME", "ID"}, [][]string{{"研发中心", "org-1"}, {"Cafe\u0301", "org-2"}, {"web\tserver\n", "org-3"}}); err != nil {
		t.Fatal(err)
	}
	want := "NAME         ID\n研发中心     org-1\nCafe\u0301         org-2\nweb server   org-3\n"
	if stdout.String() != want {
		t.Fatalf("got %q; want %q", stdout.String(), want)
	}
}

func TestAliasListLoggedOutDoesNotQueryResources(t *testing.T) {
	store := aliasListStore(t, map[string]projectconfig.Alias{"order": {Asset: "asset-1"}})
	service := aliasListResources{
		organizations: func(context.Context, string) ([]jumpserver.Organization, error) {
			t.Fatal("logged-out organization query")
			return nil, nil
		},
		find: func(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
			t.Fatal("logged-out asset query")
			return jumpserver.AssetDetail{}, nil
		},
	}
	var stdout bytes.Buffer
	root := NewRoot(Dependencies{Store: store, Resources: service, Auth: &fakeAuthService{}, Stdout: &stdout})
	root.SetArgs([]string{"alias", "list", "--profile", "work"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "asset-1（名称暂不可用）") {
		t.Fatal(stdout.String())
	}
}

func TestAliasListCancellationStopsFurtherQueries(t *testing.T) {
	store := aliasListStore(t, map[string]projectconfig.Alias{"a": {Asset: "asset-1"}, "b": {Asset: "asset-2"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	service := aliasListResources{
		organizations: func(context.Context, string) ([]jumpserver.Organization, error) { return nil, nil },
		find: func(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
			calls++
			cancel()
			return jumpserver.AssetDetail{}, ctx.Err()
		},
	}
	root := NewRoot(Dependencies{Store: store, Resources: service})
	root.SetArgs([]string{"alias", "list", "--profile", "work"})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("queries = %d", calls)
	}
}

func TestAliasAccountLabelsPreserveAmbiguityAndMissingIdentity(t *testing.T) {
	accounts := []jumpserver.Account{
		{ID: "account-1", Name: "部署账号", Username: "deploy", Alias: "ops"},
		{ID: "account-2", Name: "另一账号", Username: "deploy"},
		{ID: "account-3", Username: "root"},
		{ID: "account-4", Alias: "backup"},
		{ID: "account-5"},
	}
	for _, test := range []struct{ reference, want string }{
		{"account-1", "部署账号（deploy）"}, {"OPS", "部署账号（deploy）"},
		{"部署账号", "部署账号（deploy）"}, {"deploy", "deploy（名称暂不可用）"},
		{"account-3", "root"}, {"account-4", "backup"},
		{"account-5", "account-5（名称未提供）"}, {"missing", "missing（名称暂不可用）"},
		{"", "未绑定"}, {"@anon", "@ANON"},
	} {
		if got := aliasAccountLabel(accounts, test.reference); got != test.want {
			t.Errorf("%q: got %q, want %q", test.reference, got, test.want)
		}
	}
}

func TestAliasListOrganizationFailureDoesNotHideResolvedAsset(t *testing.T) {
	store := aliasListStore(t, map[string]projectconfig.Alias{"order": {Asset: "asset-1", Account: "account-1"}})
	service := aliasListResources{
		organizations: func(context.Context, string) ([]jumpserver.Organization, error) {
			return nil, errors.New("unavailable")
		},
		find: func(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
			return jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1", Address: "10.0.0.1"}, Accounts: []jumpserver.Account{{ID: "account-1", Username: "root"}}}, nil
		},
	}
	var stdout bytes.Buffer
	root := NewRoot(Dependencies{Store: store, Resources: service, Stdout: &stdout})
	root.SetArgs([]string{"alias", "list", "--profile", "work"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"asset-1（名称未提供）", "10.0.0.1", "root", "org-prod（名称暂不可用）（继承）"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in %s", want, stdout.String())
		}
	}
}
