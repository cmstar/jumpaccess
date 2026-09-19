package settings

import (
	"path/filepath"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

func TestCreateAliasPreservesExistingValueWhileSetAliasReplaces(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	service := Service{Store: store}
	if err := service.AddProfile("work", "https://jump.example.test"); err != nil {
		t.Fatal(err)
	}
	original := projectconfig.Alias{Asset: "original", Account: "account-one", Organization: "org-one"}
	replacement := projectconfig.Alias{Asset: "replacement", Account: "account-two", Organization: "org-two"}
	if err := service.CreateAlias("", "production", original); err != nil {
		t.Fatal(err)
	}
	if err := service.CreateAlias("work", "production", replacement); err == nil {
		t.Fatal("CreateAlias replaced existing alias")
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].Aliases["production"] != original {
		t.Fatal("CreateAlias changed existing alias")
	}
	if err := service.SetAlias("work", "production", replacement); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].Aliases["production"] != replacement {
		t.Fatal("SetAlias did not replace existing alias")
	}
}
