package connect

import (
	"context"
	"errors"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/cmstar/jumpaccess/internal/target"
)

type staticConfig struct{ value projectconfig.Config }

func (s staticConfig) Load() (projectconfig.Config, error) { return s.value, nil }

type staticTokens struct{ token credential.Token }

func (s staticTokens) EnsureFresh(context.Context, string) (credential.Token, error) {
	return s.token, nil
}

type fakeAPI struct {
	page              jumpserver.AssetPage
	detail            jumpserver.AssetDetail
	connection        jumpserver.ClientConnection
	connectionRequest jumpserver.ConnectionRequest
}

func (f *fakeAPI) ListAssets(context.Context, jumpserver.AssetQuery) (jumpserver.AssetPage, error) {
	return f.page, nil
}

func (f *fakeAPI) GetAsset(context.Context, string) (jumpserver.AssetDetail, error) {
	return f.detail, nil
}

func (f *fakeAPI) CreateConnectionToken(_ context.Context, request jumpserver.ConnectionRequest) (string, error) {
	f.connectionRequest = request
	return "connection-1", nil
}

func (f *fakeAPI) GetClientConnection(context.Context, string) (jumpserver.ClientConnection, error) {
	return f.connection, nil
}

func TestPrepareResolvesAliasAccountAndReturnsGatewayCredential(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL: "https://jump.example.test",
		Aliases: map[string]projectconfig.Alias{
			"web": {Asset: "asset-1", Account: "root", Organization: "org-1"},
		},
	}
	api := &fakeAPI{
		page: jumpserver.AssetPage{Results: []jumpserver.Asset{{ID: "asset-1", Name: "web-01", Address: "10.0.0.1"}}},
		detail: jumpserver.AssetDetail{
			Asset:     jumpserver.Asset{ID: "asset-1", Name: "web-01"},
			Accounts:  []jumpserver.Account{{ID: "account-1", Name: "root", Username: "root"}},
			Protocols: []jumpserver.Protocol{{Name: "ssh", Port: 22}},
		},
		connection: jumpserver.ClientConnection{Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: "gateway.example.test", Port: 2222}, Token: jumpserver.ConnectionCredential{ID: "connection-1", Value: "secret"}},
	}
	service := Service{
		Config: staticConfig{value: configuration}, Tokens: staticTokens{token: credential.Token{AccessToken: "oauth-access"}},
		NewAPI: func(site, accessToken, organization string) (API, error) {
			if site != "https://jump.example.test" || accessToken != "oauth-access" || organization != "org-1" {
				t.Fatalf("factory arguments = %q %q %q", site, accessToken, organization)
			}
			return api, nil
		},
	}

	prepared, err := service.Prepare(context.Background(), Options{Target: target.Input{Target: "web"}, NonInteractive: true})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Asset.ID != "asset-1" || prepared.Account.ID != "account-1" || prepared.Connection.Endpoint.Host != "gateway.example.test" {
		t.Fatalf("prepared = %#v", prepared)
	}
	if api.connectionRequest.Asset != "asset-1" || api.connectionRequest.Account != "account-1" {
		t.Fatalf("connection request = %#v", api.connectionRequest)
	}
}

func TestPrepareRejectsAmbiguousAccountInNonInteractiveMode(t *testing.T) {
	service, api := testService()
	api.detail.Accounts = []jumpserver.Account{{ID: "a1", Username: "root"}, {ID: "a2", Username: "ubuntu"}}

	_, err := service.Prepare(context.Background(), Options{Target: target.Input{Target: "asset-1"}, NonInteractive: true})
	if !errors.Is(err, ErrAccountAmbiguous) {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareAllowsInteractiveSelectorButRejectsInputAccountsWithoutSecretProvider(t *testing.T) {
	service, api := testService()
	api.detail.Accounts = []jumpserver.Account{{ID: "@INPUT", Username: "@INPUT"}, {ID: "managed", Username: "root"}}
	selected := false
	_, err := service.Prepare(context.Background(), Options{
		Target: target.Input{Target: "asset-1"},
		SelectAccount: func(accounts []jumpserver.Account) (jumpserver.Account, error) {
			selected = true
			return accounts[0], nil
		},
	})
	if !selected || !errors.Is(err, ErrInteractiveCredentialRequired) {
		t.Fatalf("selected = %v, error = %v", selected, err)
	}
}

func testService() (Service, *fakeAPI) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	api := &fakeAPI{
		page: jumpserver.AssetPage{Results: []jumpserver.Asset{{ID: "asset-1", Name: "web"}}},
		detail: jumpserver.AssetDetail{
			Asset: jumpserver.Asset{ID: "asset-1", Name: "web"}, Accounts: []jumpserver.Account{{ID: "account-1", Username: "root"}},
			Protocols: []jumpserver.Protocol{{Name: "ssh", Port: 22}},
		},
		connection: jumpserver.ClientConnection{Protocol: "ssh", Endpoint: jumpserver.Endpoint{Host: "gateway", Port: 22}, Token: jumpserver.ConnectionCredential{ID: "id", Value: "secret"}},
	}
	service := Service{
		Config: staticConfig{value: configuration}, Tokens: staticTokens{token: credential.Token{AccessToken: "access"}},
		NewAPI: func(string, string, string) (API, error) { return api, nil },
	}
	return service, api
}
