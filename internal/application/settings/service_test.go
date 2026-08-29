package settings

import (
	"path/filepath"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

func TestAddProfilePersistsAndMakesFirstProfileCurrent(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}

	if err := service.AddProfile("work", "https://jump.example.test/"); err != nil {
		t.Fatalf("AddProfile returned error: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got.CurrentProfile != "work" {
		t.Fatalf("CurrentProfile = %q, want work", got.CurrentProfile)
	}
	if got.Profiles["work"].URL != "https://jump.example.test" {
		t.Fatalf("profile URL = %q, want normalized URL", got.Profiles["work"].URL)
	}
}

func TestAddProfileRejectsDuplicateName(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://one.example.test"); err != nil {
		t.Fatalf("first AddProfile returned error: %v", err)
	}

	if err := service.AddProfile("work", "https://two.example.test"); err == nil {
		t.Fatal("second AddProfile error = nil, want duplicate error")
	}
}

func TestUseProfilePersistsCurrentProfile(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("one", "https://one.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.AddProfile("two", "https://two.example.test"); err != nil {
		t.Fatal(err)
	}

	if err := service.UseProfile("two"); err != nil {
		t.Fatalf("UseProfile returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentProfile != "two" {
		t.Fatalf("CurrentProfile = %q, want two", got.CurrentProfile)
	}
}

func TestSetAliasUsesCurrentProfile(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}

	if err := service.SetAlias("", "production", projectconfig.Alias{
		Asset:        "asset-1",
		Account:      "root",
		Organization: "org-1",
	}); err != nil {
		t.Fatalf("SetAlias returned error: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	alias := got.Profiles["work"].Aliases["production"]
	if alias.Asset != "asset-1" || alias.Account != "root" || alias.Organization != "org-1" {
		t.Fatalf("alias = %#v, want persisted mapping", alias)
	}
}

func TestSetProfileOrganizationUsesCurrentProfile(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}

	if err := service.SetProfileOrganization("", "org-1"); err != nil {
		t.Fatalf("SetProfileOrganization returned error: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].Organization != "org-1" {
		t.Fatalf("Organization = %q, want org-1", got.Profiles["work"].Organization)
	}
}

func TestDeleteAliasUsesCurrentProfile(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAlias("", "production", projectconfig.Alias{Asset: "asset-1"}); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteAlias("", "production"); err != nil {
		t.Fatalf("DeleteAlias returned error: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got.Profiles["work"].Aliases["production"]; exists {
		t.Fatal("Alias production still exists after DeleteAlias")
	}
}

func TestDeleteAliasRejectsUnknownAlias(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteAlias("", "missing"); err == nil {
		t.Fatal("DeleteAlias error = nil, want missing alias error")
	}
}

func TestSetAliasAccountPreservesAssetAndOrganization(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAlias("", "production", projectconfig.Alias{
		Asset:        "asset-1",
		Organization: "org-1",
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.SetAliasAccount("", "production", "ops"); err != nil {
		t.Fatalf("SetAliasAccount returned error: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	alias := got.Profiles["work"].Aliases["production"]
	if alias.Asset != "asset-1" || alias.Organization != "org-1" || alias.Account != "ops" {
		t.Fatalf("alias = %#v, want original target with account ops", alias)
	}
	if err := service.SetAliasAccount("", "production", ""); err != nil {
		t.Fatalf("clear SetAliasAccount returned error: %v", err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].Aliases["production"].Account != "" {
		t.Fatal("empty account did not restore ask-on-connect behavior")
	}
}
