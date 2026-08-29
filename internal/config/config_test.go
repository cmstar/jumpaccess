package config

import (
	"testing"
	"time"
)

func TestDecodeLoadsProfileScopedAlias(t *testing.T) {
	raw := []byte(`
version = 1
current_profile = "work"

[behavior]
refresh_check_interval = "30s"
refresh_before_expiry = "1m"
connect_timeout = "20s"
oauth_timeout = "5m"

[profiles.work]
url = "https://jump.example.test/"
organization = "org-production"

[profiles.work.aliases.production]
asset = "asset-web-01"
account = "root"
`)

	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	if got.CurrentProfile != "work" {
		t.Fatalf("CurrentProfile = %q, want work", got.CurrentProfile)
	}
	if got.Behavior.RefreshBeforeExpiry.Duration != time.Minute {
		t.Fatalf("RefreshBeforeExpiry = %s, want 1m", got.Behavior.RefreshBeforeExpiry.Duration)
	}
	profile := got.Profiles["work"]
	if profile.URL != "https://jump.example.test" {
		t.Fatalf("profile URL = %q, want normalized URL", profile.URL)
	}
	alias := profile.Aliases["production"]
	if alias.Asset != "asset-web-01" || alias.Account != "root" {
		t.Fatalf("alias = %#v, want asset/account mapping", alias)
	}
}

func TestDecodeRejectsUnknownCredentialField(t *testing.T) {
	raw := []byte(`
version = 1

[profiles.work]
url = "https://jump.example.test"
access_token = "must-not-be-stored-here"
`)

	if _, err := Decode(raw); err == nil {
		t.Fatal("Decode error = nil, want rejection of unknown credential field")
	}
}

func TestDecodeRejectsMissingCurrentProfile(t *testing.T) {
	raw := []byte(`
version = 1
current_profile = "missing"
`)

	if _, err := Decode(raw); err == nil {
		t.Fatal("Decode error = nil, want missing current profile error")
	}
}

func TestDecodeRejectsInvalidProfileURL(t *testing.T) {
	raw := []byte(`
version = 1

[profiles.work]
url = "file:///tmp/not-a-jumpserver"
`)

	if _, err := Decode(raw); err == nil {
		t.Fatal("Decode error = nil, want invalid profile URL error")
	}
}

func TestDecodeRejectsAliasWithoutAsset(t *testing.T) {
	raw := []byte(`
version = 1

[profiles.work]
url = "https://jump.example.test"

[profiles.work.aliases.production]
account = "root"
`)

	if _, err := Decode(raw); err == nil {
		t.Fatal("Decode error = nil, want alias asset error")
	}
}

func TestDecodeAppliesBehaviorDefaults(t *testing.T) {
	got, err := Decode(nil)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	if got.Behavior.RefreshCheckInterval.Duration != 30*time.Second {
		t.Fatalf("RefreshCheckInterval = %s, want 30s", got.Behavior.RefreshCheckInterval.Duration)
	}
	if got.Behavior.RefreshBeforeExpiry.Duration != time.Minute {
		t.Fatalf("RefreshBeforeExpiry = %s, want 1m", got.Behavior.RefreshBeforeExpiry.Duration)
	}
	if got.Behavior.ConnectTimeout.Duration != 30*time.Second {
		t.Fatalf("ConnectTimeout = %s, want 30s", got.Behavior.ConnectTimeout.Duration)
	}
	if got.Behavior.OAuthTimeout.Duration != 5*time.Minute {
		t.Fatalf("OAuthTimeout = %s, want 5m", got.Behavior.OAuthTimeout.Duration)
	}
}

func TestDecodeRejectsUnsupportedVersion(t *testing.T) {
	if _, err := Decode([]byte("version = 2\n")); err == nil {
		t.Fatal("Decode error = nil, want unsupported version error")
	}
}

func TestDecodeRejectsNonPositiveBehaviorDuration(t *testing.T) {
	fields := []string{"refresh_check_interval", "refresh_before_expiry", "connect_timeout", "oauth_timeout"}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			raw := []byte("version = 1\n\n[behavior]\n" + field + " = \"0s\"\n")
			if _, err := Decode(raw); err == nil {
				t.Fatal("Decode error = nil, want non-positive duration error")
			}
		})
	}
}

func TestDecodeAcceptsProfileNameIndependentlyOfFilesystemRules(t *testing.T) {
	data := []byte(`
version = 1
current_profile = "team/研发:CON"

[behavior]
refresh_check_interval = "30s"
refresh_before_expiry = "1m"
connect_timeout = "30s"
oauth_timeout = "5m"

[profiles."team/研发:CON"]
url = "https://jump.example.test"
`)
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if _, ok := got.Profiles["team/研发:CON"]; !ok {
		t.Fatalf("Profiles = %#v, want original profile name", got.Profiles)
	}
}

func TestDecodeRejectsAmbiguousProfileName(t *testing.T) {
	for _, name := range []string{" ", " work", "work ", ".", "..", "line\nbreak"} {
		t.Run(name, func(t *testing.T) {
			value := Default()
			value.Profiles[name] = Profile{URL: "https://jump.example.test"}
			if err := value.Validate(); err == nil {
				t.Fatalf("Validate unexpectedly accepted profile name %q", name)
			}
		})
	}
}
