package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	settingsapp "github.com/cmstar/jumpaccess/internal/application/settings"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

func TestAliasAccountChangesRejectAmbiguityWithoutSaving(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
			configuration := projectconfig.Default()
			configuration.CurrentProfile = "work"
			configuration.Profiles["work"] = projectconfig.Profile{
				URL: "https://jump.example.test", Organization: "org-1",
				Aliases: map[string]projectconfig.Alias{"production": {Asset: "asset-1", Account: "account-1", Organization: "org-1"}},
			}
			if err := store.Save(configuration); err != nil {
				t.Fatal(err)
			}
			before, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			resources := &fakeResources{details: map[string]jumpserver.AssetDetail{
				"asset-1": {Asset: jumpserver.Asset{ID: "asset-1"}, Accounts: []jumpserver.Account{
					{ID: "account-1", Username: "deploy"}, {ID: "account-2", Username: "DEPLOY"},
				}},
			}}
			service := Service{Config: store, Resources: resources, Settings: settingsapp.Service{Store: store}}
			if operation == "create" {
				_, err = service.CreateAlias(context.Background(), CreateAliasRequest{Asset: "asset-1", Name: "new", Account: "deploy"})
			} else {
				err = service.SetAliasAccount(context.Background(), AliasAccountRequest{Name: "production", Account: "deploy"})
			}
			if !errors.Is(err, connectapp.ErrAccountAmbiguous) {
				t.Fatalf("ambiguous account error = %v", err)
			}
			after, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected account selection changed configuration")
			}
		})
	}
}

func TestResolveAliasAccountKeepsOptionalBindingAndPermittedFallback(t *testing.T) {
	for _, test := range []struct {
		name      string
		accounts  []jumpserver.Account
		reference string
		want      string
		wantErr   error
	}{
		{name: "optional", accounts: []jumpserver.Account{{ID: "account-1"}}, want: ""},
		{name: "username fallback", accounts: []jumpserver.Account{{Username: "deploy"}}, reference: "DEPLOY", want: "deploy"},
		{name: "unauthorized pseudo account", reference: "@ANON", wantErr: connectapp.ErrAccountNotFound},
		{name: "permitted pseudo account", accounts: []jumpserver.Account{{ID: "@ANON"}}, reference: "@ANON", want: "@ANON"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveAccount(test.accounts, test.reference)
			if got != test.want || !errors.Is(err, test.wantErr) {
				t.Fatalf("account = %q, error = %v", got, err)
			}
		})
	}
}
