package config

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreUpdateSerializesReadModifyWrite(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.Update(context.Background(), func(value *Config) error {
			close(firstStarted)
			<-releaseFirst
			value.Profiles["one"] = Profile{URL: "https://one.example.test"}
			return nil
		})
	}()
	<-firstStarted

	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- store.Update(context.Background(), func(value *Config) error {
			close(secondStarted)
			value.Profiles["two"] = Profile{URL: "https://two.example.test"}
			return nil
		})
	}()
	select {
	case <-secondStarted:
		t.Fatal("second update started before first update released the config lock")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Profiles["one"]; !ok {
		t.Fatal("first update was lost")
	}
	if _, ok := got.Profiles["two"]; !ok {
		t.Fatal("second update was lost")
	}
}

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
