package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type fakeAuthService struct {
	loginProfile   string
	loginOptions   authapp.LoginOptions
	status         authapp.Status
	refreshProfile string
	logoutProfile  string
}

type fakeConnectionPreparer struct {
	options  connectapp.Options
	prepared connectapp.Prepared
}

type fakeResourceService struct {
	organizations []jumpserver.Organization
	assets        jumpserver.AssetPage
	asset         jumpserver.AssetDetail
}

func (f fakeResourceService) ListOrganizations(context.Context, string) ([]jumpserver.Organization, error) {
	return f.organizations, nil
}

func (f fakeResourceService) ListAssets(context.Context, string, string, string) (jumpserver.AssetPage, error) {
	return f.assets, nil
}

func (f fakeResourceService) FindAsset(context.Context, string, string, string) (jumpserver.AssetDetail, error) {
	return f.asset, nil
}

func (f *fakeConnectionPreparer) Prepare(_ context.Context, options connectapp.Options) (connectapp.Prepared, error) {
	f.options = options
	return f.prepared, nil
}

func (f *fakeAuthService) Login(_ context.Context, profile string, options authapp.LoginOptions) (authapp.Status, error) {
	f.loginProfile = profile
	f.loginOptions = options
	return f.status, nil
}

func (f *fakeAuthService) Status(string) (authapp.Status, error) { return f.status, nil }

func (f *fakeAuthService) Refresh(_ context.Context, profile string) (authapp.Status, error) {
	f.refreshProfile = profile
	return f.status, nil
}

func (f *fakeAuthService) Logout(_ context.Context, profile string) error {
	f.logoutProfile = profile
	return nil
}

func TestVersionCommandPrintsBinaryVersion(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRoot(Dependencies{
		Version:    "1.2.3-test",
		ConfigPath: filepath.Join(t.TempDir(), "config.toml"),
		Store:      projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")},
		Stdout:     &stdout,
	})
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got, want := stdout.String(), "jumpctl 1.2.3-test\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestLicensesCommandPrintsProjectAndThirdPartyNotices(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRoot(Dependencies{
		Licenses: "PROJECT LICENSE\n\nTHIRD-PARTY NOTICES\n",
		Stdout:   &stdout,
	})
	root.SetArgs([]string{"licenses"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got, want := stdout.String(), "PROJECT LICENSE\n\nTHIRD-PARTY NOTICES\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestConfigPathPrintsTheEditableTOMLPath(t *testing.T) {
	var stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	root := NewRoot(Dependencies{
		ConfigPath: path,
		Store:      projectconfig.Store{Path: path},
		Stdout:     &stdout,
	})
	root.SetArgs([]string{"config", "path"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got, want := stdout.String(), path+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestConfigEditCreatesDefaultsBeforeOpeningFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	var opened string
	root := NewRoot(Dependencies{
		ConfigPath: path,
		Store:      projectconfig.Store{Path: path},
		OpenFile: func(value string) error {
			opened = value
			return nil
		},
	})
	root.SetArgs([]string{"config", "edit"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if opened != path {
		t.Fatalf("opened path = %q, want %q", opened, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
}

func TestConfigValidateReportsValidFile(t *testing.T) {
	var stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	store := projectconfig.Store{Path: path}
	if err := store.Save(projectconfig.Default()); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Dependencies{Store: store, ConfigPath: path, Stdout: &stdout})
	root.SetArgs([]string{"config", "validate"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if stdout.String() != "config valid\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestProfileAddPersistsProfile(t *testing.T) {
	var stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	store := projectconfig.Store{Path: path}
	root := NewRoot(Dependencies{Store: store, ConfigPath: path, Stdout: &stdout})
	root.SetArgs([]string{"profile", "add", "work", "--url", "https://jump.example.test"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].URL != "https://jump.example.test" {
		t.Fatalf("profile was not persisted: %#v", got.Profiles)
	}
	if stdout.String() != "profile work added\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestProfileListSortsAndMarksCurrentProfile(t *testing.T) {
	var stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	store := projectconfig.Store{Path: path}
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://work.example.test"}
	value.Profiles["backup"] = projectconfig.Profile{URL: "https://backup.example.test"}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Dependencies{Store: store, ConfigPath: path, Stdout: &stdout})
	root.SetArgs([]string{"profile", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	const want = "CURRENT  PROFILE  URL\n" +
		"         backup   https://backup.example.test\n" +
		"*        work     https://work.example.test\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestProfileUseChangesCurrentProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	store := projectconfig.Store{Path: path}
	value := projectconfig.Default()
	value.CurrentProfile = "one"
	value.Profiles["one"] = projectconfig.Profile{URL: "https://one.example.test"}
	value.Profiles["two"] = projectconfig.Profile{URL: "https://two.example.test"}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Dependencies{Store: store, ConfigPath: path})
	root.SetArgs([]string{"profile", "use", "two"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentProfile != "two" {
		t.Fatalf("CurrentProfile = %q, want two", got.CurrentProfile)
	}
}

func TestAliasSetPersistsProfileScopedAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	store := projectconfig.Store{Path: path}
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Dependencies{Store: store, ConfigPath: path})
	root.SetArgs([]string{
		"alias", "set", "production",
		"--asset", "asset-1",
		"--account", "root",
		"--organization", "org-1",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	alias := got.Profiles["work"].Aliases["production"]
	if alias.Asset != "asset-1" || alias.Account != "root" || alias.Organization != "org-1" {
		t.Fatalf("alias = %#v, want persisted values", alias)
	}
}

func TestAliasListUsesCurrentProfileAndSortsNames(t *testing.T) {
	var stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	store := projectconfig.Store{Path: path}
	value := projectconfig.Default()
	value.CurrentProfile = "work"
	value.Profiles["work"] = projectconfig.Profile{
		URL: "https://jump.example.test",
		Aliases: map[string]projectconfig.Alias{
			"web": {Asset: "asset-web", Account: "root"},
			"db":  {Asset: "asset-db", Account: "dba", Organization: "org-db"},
		},
	}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	root := NewRoot(Dependencies{Store: store, ConfigPath: path, Stdout: &stdout})
	root.SetArgs([]string{"alias", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	const want = "ALIAS  ASSET      ACCOUNT  ORGANIZATION\n" +
		"db     asset-db   dba      org-db\n" +
		"web    asset-web  root\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestAuthLoginUsesBrowserServiceWithoutPrintingSecrets(t *testing.T) {
	var stdout bytes.Buffer
	service := &fakeAuthService{status: authapp.Status{Profile: "work", LoggedIn: true}}
	root := NewRoot(Dependencies{Auth: service, Stdout: &stdout})
	root.SetArgs([]string{"auth", "login", "--profile", "work"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if service.loginProfile != "work" || stdout.String() != "authenticated profile work\n" {
		t.Fatalf("profile = %q, stdout = %q", service.loginProfile, stdout.String())
	}
}

func TestAuthLoginManualFlagForcesPastedCallbackFlow(t *testing.T) {
	service := &fakeAuthService{status: authapp.Status{Profile: "work", LoggedIn: true}}
	root := NewRoot(Dependencies{Auth: service})
	root.SetArgs([]string{"auth", "login", "--profile", "work", "--manual"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !service.loginOptions.Manual {
		t.Fatal("manual login option was not passed to the authentication service")
	}
}

func TestAuthStatusReportsExpiryWithoutPrintingTokens(t *testing.T) {
	var stdout bytes.Buffer
	expires := time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC)
	service := &fakeAuthService{status: authapp.Status{Profile: "work", LoggedIn: true, RefreshAvailable: true, ExpiresAt: expires}}
	root := NewRoot(Dependencies{Auth: service, Stdout: &stdout})
	root.SetArgs([]string{"auth", "status"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	const want = "profile: work\nstatus: authenticated\naccess token expires: 2026-08-27T13:00:00Z\nrefresh available: yes\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestAuthRefreshAndLogoutPassSelectedProfile(t *testing.T) {
	service := &fakeAuthService{status: authapp.Status{Profile: "work", LoggedIn: true}}
	for _, args := range [][]string{
		{"auth", "refresh", "--profile", "work"},
		{"auth", "logout", "--profile", "work"},
	} {
		root := NewRoot(Dependencies{Auth: service})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute(%v): %v", args, err)
		}
	}
	if service.refreshProfile != "work" || service.logoutProfile != "work" {
		t.Fatalf("refresh = %q, logout = %q", service.refreshProfile, service.logoutProfile)
	}
}

func TestSSHCommandPreparesInteractiveTargetAndRunsClient(t *testing.T) {
	preparer := &fakeConnectionPreparer{prepared: connectapp.Prepared{
		Connection: jumpserver.ClientConnection{Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: "gateway", Port: 22}},
	}}
	var ran jumpserver.ClientConnection
	root := NewRoot(Dependencies{
		Connect: preparer,
		RunSSH: func(_ context.Context, prepared connectapp.Prepared) error {
			ran = prepared.Connection
			return nil
		},
	})
	root.SetArgs([]string{"ssh", "web", "--profile", "work", "--organization", "org-1", "--account", "root"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if preparer.options.NonInteractive || preparer.options.Target.Target != "web" || preparer.options.Target.Profile != "work" || preparer.options.Target.Organization != "org-1" || preparer.options.Target.Account != "root" {
		t.Fatalf("options = %#v", preparer.options)
	}
	if ran.Endpoint.Host != "gateway" {
		t.Fatalf("ran connection = %#v", ran)
	}
}

func TestProxyCommandPreflightsNonInteractivelyAndKeepsStdoutUnused(t *testing.T) {
	var stdout bytes.Buffer
	preparer := &fakeConnectionPreparer{prepared: connectapp.Prepared{
		Connection: jumpserver.ClientConnection{Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: "gateway", Port: 22}},
	}}
	called := false
	root := NewRoot(Dependencies{
		Stdout: &stdout, Connect: preparer,
		RunProxy: func(_ context.Context, prepared connectapp.Prepared) error {
			called = prepared.Connection.Endpoint.Host == "gateway"
			return nil
		},
	})
	root.SetArgs([]string{"proxy", "web", "--account", "root"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called || !preparer.options.NonInteractive || preparer.options.SelectAccount != nil {
		t.Fatalf("called = %v, options = %#v", called, preparer.options)
	}
	if stdout.Len() != 0 {
		t.Fatalf("proxy command wrote non-protocol stdout: %q", stdout.String())
	}
}

func TestResourceCommandsPrintOrganizationsAssetsAndAccounts(t *testing.T) {
	service := fakeResourceService{
		organizations: []jumpserver.Organization{{ID: "org-2", Name: "Two"}, {ID: "org-1", Name: "One"}},
		assets: jumpserver.AssetPage{Results: []jumpserver.Asset{
			{ID: "asset-1", Name: "web", Address: "10.0.0.1", Type: jumpserver.LabelValue{Value: "linux"}},
		}},
		asset: jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1", Name: "web"}, Accounts: []jumpserver.Account{{ID: "account-1", Username: "root", Name: "Root"}}},
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"organization", "list"}, "ID     NAME\norg-1  One\norg-2  Two\n"},
		{[]string{"asset", "list", "--search", "web"}, "ID       NAME  ADDRESS   TYPE\nasset-1  web   10.0.0.1  linux\n"},
		{[]string{"account", "list", "web"}, "ID         USERNAME  NAME\naccount-1  root      Root\n"},
	} {
		var stdout bytes.Buffer
		root := NewRoot(Dependencies{Resources: service, Stdout: &stdout})
		root.SetArgs(test.args)
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute(%v): %v", test.args, err)
		}
		if stdout.String() != test.want {
			t.Fatalf("Execute(%v) stdout = %q, want %q", test.args, stdout.String(), test.want)
		}
	}
}

func TestResourceListPrintsHeaderWhenThereAreNoResults(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRoot(Dependencies{Resources: fakeResourceService{}, Stdout: &stdout})
	root.SetArgs([]string{"asset", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got, want := stdout.String(), "ID  NAME  ADDRESS  TYPE\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
