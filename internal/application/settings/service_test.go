package settings

import (
	"path/filepath"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

type recordingCredentialRemover struct {
	profiles []string
}

func (r *recordingCredentialRemover) Delete(profile string) error {
	r.profiles = append(r.profiles, profile)
	return nil
}

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

func TestUpdateProfileURLPreservesLocalContextAndRemovesCredential(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	credentials := &recordingCredentialRemover{}
	service := Service{Store: store, Credentials: credentials}
	if err := service.AddProfile("work", "https://old.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetProfileOrganization("work", "org-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAlias("work", "production", projectconfig.Alias{Asset: "asset-1", Account: "account-1", Organization: "org-1"}); err != nil {
		t.Fatal(err)
	}

	if err := service.UpdateProfileURL("work", "https://new.example.test/"); err != nil {
		t.Fatalf("UpdateProfileURL returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	profile := got.Profiles["work"]
	if profile.URL != "https://new.example.test" || profile.Organization != "org-1" {
		t.Fatalf("updated profile = %#v", profile)
	}
	if profile.Aliases["production"] != (projectconfig.Alias{Asset: "asset-1", Account: "account-1", Organization: "org-1"}) {
		t.Fatalf("updated profile aliases = %#v", profile.Aliases)
	}
	if got.CurrentProfile != "work" {
		t.Fatalf("CurrentProfile = %q, want work", got.CurrentProfile)
	}
	if len(credentials.profiles) != 1 || credentials.profiles[0] != "work" {
		t.Fatalf("deleted credentials = %#v, want work", credentials.profiles)
	}
}

func TestUpdateProfileURLDoesNotRemoveCredentialForEquivalentURL(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	credentials := &recordingCredentialRemover{}
	service := Service{Store: store, Credentials: credentials}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}

	if err := service.UpdateProfileURL("work", "https://jump.example.test/"); err != nil {
		t.Fatalf("UpdateProfileURL returned error: %v", err)
	}
	if len(credentials.profiles) != 0 {
		t.Fatalf("deleted credentials = %#v, want none", credentials.profiles)
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

func TestDeleteProfileRemovesCredentialAndSelectsNextProfile(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	credentials := &recordingCredentialRemover{}
	service := Service{Store: store, Credentials: credentials}
	if err := service.AddProfile("work", "https://work.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.AddProfile("backup", "https://backup.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAlias("work", "production", projectconfig.Alias{Asset: "asset-1"}); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteProfile("work"); err != nil {
		t.Fatalf("DeleteProfile returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got.Profiles["work"]; exists {
		t.Fatal("profile work still exists after DeleteProfile")
	}
	if got.CurrentProfile != "backup" {
		t.Fatalf("CurrentProfile = %q, want backup", got.CurrentProfile)
	}
	if len(credentials.profiles) != 1 || credentials.profiles[0] != "work" {
		t.Fatalf("deleted credentials = %#v, want work", credentials.profiles)
	}

	if err := service.DeleteProfile("backup"); err != nil {
		t.Fatalf("delete last profile: %v", err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentProfile != "" || len(got.Profiles) != 0 {
		t.Fatalf("config after deleting last profile = %#v", got)
	}
}

func TestDeleteProfileRejectsUnknownProfileWithoutDeletingCredential(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	credentials := &recordingCredentialRemover{}
	service := Service{Store: store, Credentials: credentials}

	if err := service.DeleteProfile("missing"); err == nil {
		t.Fatal("DeleteProfile error = nil, want missing profile error")
	}
	if len(credentials.profiles) != 0 {
		t.Fatalf("deleted credentials = %#v, want none", credentials.profiles)
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
