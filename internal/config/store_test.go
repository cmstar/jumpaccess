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
