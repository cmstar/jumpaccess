package config

import (
	"path/filepath"
	"testing"
)

func TestStoreSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	store := Store{Path: path}
	want := Default()
	want.CurrentProfile = "work"
	want.Profiles["work"] = Profile{
		URL:          "https://jump.example.test",
		Organization: "org-1",
		Aliases: map[string]Alias{
			"production": {Asset: "asset-1", Account: "account-1"},
		},
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got.CurrentProfile != "work" {
		t.Fatalf("CurrentProfile = %q, want work", got.CurrentProfile)
	}
	if got.Profiles["work"].Aliases["production"].Account != "account-1" {
		t.Fatalf("round-trip alias = %#v", got.Profiles["work"].Aliases["production"])
	}
}

func TestStoreLoadMissingReturnsDefaultConfig(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got.Version != CurrentVersion || got.Behavior.RefreshBeforeExpiry.Duration == 0 {
		t.Fatalf("Load missing config = %#v, want defaults", got)
	}
}

func TestStoreRoundTripsFilesystemIndependentProfileName(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	want := Default()
	want.CurrentProfile = "team/研发:CON"
	want.Profiles[want.CurrentProfile] = Profile{URL: "https://jump.example.test"}

	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Profiles[want.CurrentProfile]; !ok || got.CurrentProfile != want.CurrentProfile {
		t.Fatalf("round-trip config = %#v, want profile %q", got, want.CurrentProfile)
	}
}
