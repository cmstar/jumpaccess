package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

type fakeAuthService struct {
	loginProfile   string
	status         authapp.Status
	refreshProfile string
	logoutProfile  string
}

func (f *fakeAuthService) Login(_ context.Context, profile string) (authapp.Status, error) {
	f.loginProfile = profile
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
	const want = "  backup\thttps://backup.example.test\n* work\thttps://work.example.test\n"
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
	const want = "db\tasset-db\tdba\torg-db\nweb\tasset-web\troot\t\n"
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
