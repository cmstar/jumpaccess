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

func profileListStore(t *testing.T, names ...string) projectconfig.Store {
	t.Helper()
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	value := projectconfig.Default()
	for _, name := range names {
		value.Profiles[name] = projectconfig.Profile{URL: "https://" + name + ".example.test", Organization: "org-1"}
	}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestProfileListResolvesOrganizationNamesSeparatelyAndContinuesAfterFailure(t *testing.T) {
	store := profileListStore(t, "offline", "production", "staging")
	var calls []string
	service := fakeResourceService{listOrganizations: func(_ context.Context, profile string) ([]jumpserver.Organization, error) {
		calls = append(calls, profile)
		if profile == "offline" {
			return nil, errors.New("private lookup error")
		}
		name := map[string]string{"production": "生产环境", "staging": "测试环境"}[profile]
		return []jumpserver.Organization{{ID: "org-1", Name: name}}, nil
	}}
	var stdout, stderr bytes.Buffer
	root := NewRoot(Dependencies{Store: store, Resources: service, Stdout: &stdout, Stderr: &stderr})
	root.SetArgs([]string{"profile", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"org-1（名称暂不可用）", "生产环境", "测试环境"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("missing %q in %s", want, stdout.String())
		}
	}
	if got := strings.Join(calls, ","); got != "offline,production,staging" {
		t.Errorf("organization queries = %q", got)
	}
	if strings.Contains(stdout.String()+stderr.String(), "private lookup error") {
		t.Fatal("leaked lookup error")
	}
}

func TestProfileListOrganizationNameFallbacks(t *testing.T) {
	for _, test := range []struct {
		name        string
		noResources bool
		loggedOut   bool
		items       []jumpserver.Organization
		want        string
		wantCalls   int
	}{
		{name: "no resources", noResources: true, want: "org-1（名称暂不可用）"},
		{name: "logged out", loggedOut: true, want: "org-1（名称暂不可用）"},
		{name: "not visible", items: []jumpserver.Organization{{ID: "org-other", Name: "其他组织"}}, want: "org-1（名称暂不可用）", wantCalls: 1},
		{name: "missing name", items: []jumpserver.Organization{{ID: "org-1", Name: "  "}}, want: "名称未提供", wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			calls := 0
			deps := Dependencies{Store: profileListStore(t, "work"), Stdout: &stdout}
			if !test.noResources {
				deps.Resources = fakeResourceService{listOrganizations: func(context.Context, string) ([]jumpserver.Organization, error) {
					calls++
					return test.items, nil
				}}
			}
			if test.loggedOut {
				deps.Auth = &fakeAuthService{}
			}
			root := NewRoot(deps)
			root.SetArgs([]string{"profile", "list"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(stdout.String(), "  "+test.want+"\n") || calls != test.wantCalls {
				t.Fatalf("stdout = %q, queries = %d; want %q, %d", stdout.String(), calls, test.want, test.wantCalls)
			}
		})
	}
}

func TestProfileListCancellationStopsOrganizationQueries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	service := fakeResourceService{listOrganizations: func(context.Context, string) ([]jumpserver.Organization, error) {
		calls++
		cancel()
		return nil, ctx.Err()
	}}
	var stdout bytes.Buffer
	root := NewRoot(Dependencies{Store: profileListStore(t, "one", "two"), Resources: service, Stdout: &stdout})
	root.SetArgs([]string{"profile", "list"})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if calls != 1 || stdout.Len() != 0 {
		t.Fatalf("organization queries = %d, stdout = %q", calls, stdout.String())
	}
}
