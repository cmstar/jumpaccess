package target

import (
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

func TestResolveUsesAliasWithinSelectedProfile(t *testing.T) {
	cfg := projectconfig.Default()
	cfg.CurrentProfile = "work"
	cfg.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-default",
		Aliases: map[string]projectconfig.Alias{
			"production": {
				Asset:        "asset-1",
				Account:      "account-1",
				Organization: "org-alias",
			},
		},
	}

	got, err := Resolve(cfg, Input{Target: "production"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if got.Profile != "work" || got.Asset != "asset-1" || got.Account != "account-1" || got.Organization != "org-alias" {
		t.Fatalf("Resolve = %#v, want profile-scoped alias values", got)
	}
	if got.Alias != "production" {
		t.Fatalf("Alias = %q, want production", got.Alias)
	}
}

func TestResolveTreatsUnknownAliasAsRemoteAssetReference(t *testing.T) {
	cfg := projectconfig.Default()
	cfg.CurrentProfile = "work"
	cfg.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-default",
	}

	got, err := Resolve(cfg, Input{
		Target:       "asset-web-01",
		Organization: "org-explicit",
		Account:      "root",
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if got.Asset != "asset-web-01" || got.Account != "root" || got.Organization != "org-explicit" {
		t.Fatalf("Resolve = %#v, want explicit remote asset selection", got)
	}
	if got.Alias != "" {
		t.Fatalf("Alias = %q, want empty for remote asset reference", got.Alias)
	}
}

func TestResolveRejectsEmptyTarget(t *testing.T) {
	cfg := projectconfig.Default()
	cfg.CurrentProfile = "work"
	cfg.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}

	if _, err := Resolve(cfg, Input{}); err == nil {
		t.Fatal("Resolve error = nil, want empty target error")
	}
}
