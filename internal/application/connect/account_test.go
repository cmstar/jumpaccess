package connect

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

func TestResolvePermittedAccountUsesUniqueReference(t *testing.T) {
	accounts := []jumpserver.Account{
		{ID: "account-1", Name: "Managed", Alias: "primary", Username: "deploy"},
		{ID: "account-2", Name: "Other", Alias: "secondary", Username: "root"},
	}
	for _, reference := range []string{"account-1", "MANAGED", "PRIMARY", "DEPLOY"} {
		t.Run(reference, func(t *testing.T) {
			got, err := ResolvePermittedAccount(accounts, reference)
			if err != nil || !reflect.DeepEqual(got, accounts[0]) {
				t.Fatalf("account = %#v, error = %v", got, err)
			}
		})
	}
	for _, reference := range []string{"", "missing", "ACCOUNT-1", "@ANON"} {
		t.Run("missing_"+reference, func(t *testing.T) {
			_, err := ResolvePermittedAccount(accounts, reference)
			if !errors.Is(err, ErrAccountNotFound) {
				t.Fatalf("account error = %v", err)
			}
		})
	}
}

func TestResolvePermittedAccountRejectsCrossFieldAmbiguity(t *testing.T) {
	accounts := []jumpserver.Account{{ID: "account-1", Name: "root"}, {ID: "account-2", Username: "ROOT"}}
	_, err := ResolvePermittedAccount(accounts, "root")
	if !errors.Is(err, ErrAccountAmbiguous) {
		t.Fatalf("account error = %v", err)
	}
	_, err = resolveAccount(accounts, "root", Options{NonInteractive: true})
	if !errors.Is(err, ErrAccountAmbiguous) {
		t.Fatalf("connection account error = %v", err)
	}
}

func TestResolveAccountRetainsConnectionPseudoAccounts(t *testing.T) {
	account, err := resolveAccount(nil, "@anon", Options{NonInteractive: true})
	if err != nil || account.ID != "@ANON" {
		t.Fatalf("account = %#v, error = %v", account, err)
	}
}
