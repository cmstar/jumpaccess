package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

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
