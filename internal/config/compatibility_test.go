package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestStorePreservesUnknownFieldsWithoutResurrectingDeletedSettings(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	raw := `version = 2
current_profile = "work"
[mcp]
enabled = false
port = 0
tags = ["one", "two"]
created = 2026-10-29T12:00:00Z
[[mcp.rules]]
name = "keep"
[behavior]
future_timeout = "3m"
[profiles.work]
url = "https://jump.example.test"
organization = "clear-me"
future = { mode = "keep" }
[profiles.work.aliases."web.prod"]
asset = "asset-1"
account = "clear-me"
future = false
[profiles.work.aliases.remove]
asset = "asset-2"
future = true
[profiles.remove]
url = "https://other.example.test"
future = true
`
	if err := os.WriteFile(store.Path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	var before map[string]any
	if _, err := toml.Decode(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(value *Config) error {
		value.Version = CurrentVersion // 旧客户端提交的版本不能降低文件版本。
		profile := value.Profiles["work"]
		profile.Organization = ""
		alias := profile.Aliases["web.prod"]
		alias.Account = ""
		profile.Aliases["web.prod"] = alias
		delete(profile.Aliases, "remove")
		value.Profiles["work"] = profile
		delete(value.Profiles, "remove")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if _, err := toml.DecodeFile(store.Path, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before["mcp"], after["mcp"]) || after["version"] != int64(2) {
		t.Fatal("future configuration was lost or downgraded")
	}
	if after["behavior"].(map[string]any)["future_timeout"] != "3m" {
		t.Fatal("unknown behavior field lost")
	}
	profiles := after["profiles"].(map[string]any)
	if _, exists := profiles["remove"]; exists {
		t.Fatal("deleted profile resurrected")
	}
	profile := profiles["work"].(map[string]any)
	if _, exists := profile["organization"]; exists {
		t.Fatal("cleared organization resurrected")
	}
	if !reflect.DeepEqual(profile["future"], map[string]any{"mode": "keep"}) {
		t.Fatal("unknown profile field lost")
	}
	aliases := profile["aliases"].(map[string]any)
	if _, exists := aliases["remove"]; exists {
		t.Fatal("deleted alias resurrected")
	}
	alias := aliases["web.prod"].(map[string]any)
	if _, exists := alias["account"]; exists {
		t.Fatal("cleared account resurrected")
	}
	if alias["future"] != false {
		t.Fatal("unknown alias field lost")
	}
}

func TestStoreSaveDoesNotOverwriteMalformedFile(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	original := []byte("[mcp\n")
	if err := os.WriteFile(store.Path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("overwrote a file whose unknown fields cannot be read")
	}
	got, err := os.ReadFile(store.Path)
	if err != nil || string(got) != string(original) {
		t.Fatal("original file changed")
	}
}

func TestStoreClearsKnownFieldsWithCaseInsensitiveTOMLKeys(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	raw := "Version = 2\n[Profiles.work]\nURL = \"https://jump.example.test\"\nOrganization = \"clear-me\"\nfuture = true\n"
	if err := os.WriteFile(store.Path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), func(value *Config) error {
		value.Version = CurrentVersion
		profile := value.Profiles["work"]
		profile.Organization = ""
		value.Profiles["work"] = profile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.Version != 2 || got.Profiles["work"].Organization != "" {
		t.Fatal("case-insensitive known fields were restored or downgraded")
	}
	var fields map[string]any
	if _, err := toml.DecodeFile(store.Path, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["profiles"].(map[string]any)["work"].(map[string]any)["future"] != true {
		t.Fatal("profile extension lost")
	}
}
