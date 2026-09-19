package connect

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
	listQueries       []jumpserver.AssetQuery
	getAssetIDs       []string
	listAssets        func(jumpserver.AssetQuery) (jumpserver.AssetPage, error)
}

func (f *fakeAPI) ListAssets(_ context.Context, query jumpserver.AssetQuery) (jumpserver.AssetPage, error) {
	f.listQueries = append(f.listQueries, query)
	if f.listAssets != nil {
		return f.listAssets(query)
	}
	return f.page, nil
}

func (f *fakeAPI) GetAsset(_ context.Context, id string) (jumpserver.AssetDetail, error) {
	f.getAssetIDs = append(f.getAssetIDs, id)
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
		Config: staticConfig{value: configuration}, Tokens: staticTokens{token: credential.Token{AccessToken: "oauth-access", Site: "https://jump.example.test"}},
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

func TestResolveAssetUsesDetailEndpointForUUID(t *testing.T) {
	const assetID = "123e4567-e89b-12d3-a456-426614174000"
	api := &fakeAPI{
		detail: jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: assetID, Name: "web-01"}},
	}

	asset, err := ResolveAsset(context.Background(), api, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID != assetID {
		t.Fatalf("asset ID = %q, want %q", asset.ID, assetID)
	}
	if len(api.listQueries) != 0 {
		t.Fatalf("ListAssets queries = %#v, want no search for UUID", api.listQueries)
	}
	if len(api.getAssetIDs) != 1 || api.getAssetIDs[0] != assetID {
		t.Fatalf("GetAsset IDs = %#v, want [%q]", api.getAssetIDs, assetID)
	}
}

func TestResolveAssetSearchesNamesAndAddresses(t *testing.T) {
	for _, reference := range []string{"web-01", "10.0.0.1"} {
		t.Run(reference, func(t *testing.T) {
			api := &fakeAPI{
				page:   jumpserver.AssetPage{Results: []jumpserver.Asset{{ID: "asset-1", Name: "web-01", Address: "10.0.0.1"}}},
				detail: jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1", Name: "web-01", Address: "10.0.0.1"}},
			}

			asset, err := ResolveAsset(context.Background(), api, reference)
			if err != nil {
				t.Fatal(err)
			}
			if asset.ID != "asset-1" {
				t.Fatalf("asset ID = %q, want asset-1", asset.ID)
			}
			if len(api.listQueries) != 1 || api.listQueries[0].Search != reference {
				t.Fatalf("ListAssets queries = %#v, want search %q", api.listQueries, reference)
			}
			if len(api.getAssetIDs) != 1 || api.getAssetIDs[0] != "asset-1" {
				t.Fatalf("GetAsset IDs = %#v, want [asset-1]", api.getAssetIDs)
			}
		})
	}
}

func TestResolveAssetUsesFirstExactMatch(t *testing.T) {
	api := &fakeAPI{page: jumpserver.AssetPage{Results: []jumpserver.Asset{
		{ID: "partial", Name: "web-backup"},
		{ID: "asset-1", Name: "web"},
		{ID: "asset-2", Name: "WEB"},
	}, Count: 200, Next: "next-page"}, detail: jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1"}}}

	asset, err := ResolveAsset(context.Background(), api, "web")
	if err != nil || asset.ID != "asset-1" {
		t.Fatalf("asset = %#v, error = %v", asset, err)
	}
	if len(api.listQueries) != 1 || !reflect.DeepEqual(api.getAssetIDs, []string{"asset-1"}) {
		t.Fatalf("queries = %#v, detail IDs = %#v", api.listQueries, api.getAssetIDs)
	}
}

func TestResolveAssetPagination(t *testing.T) {
	firstPage := make([]jumpserver.Asset, 100)
	for i := range firstPage {
		firstPage[i] = jumpserver.Asset{ID: fmt.Sprintf("partial-%d", i), Name: "web-backup"}
	}
	wantAsset := jumpserver.Asset{ID: "selected", Name: "WEB", Address: "10.0.0.1"}
	requestErr := errors.New("page request failed")
	for _, tc := range []struct {
		name        string
		reference   string
		pages       map[int]jumpserver.AssetPage
		wantOffsets []int
		wantErr     error
		failOffset  int
	}{
		{
			name: "match after first hundred stops before last page", reference: "web",
			pages:       map[int]jumpserver.AssetPage{0: {Count: 250, Results: firstPage}, 100: {Count: 250, Results: []jumpserver.Asset{wantAsset}}},
			wantOffsets: []int{0, 100},
		},
		{
			name: "short page with next continues by actual length", reference: "10.0.0.1",
			pages:       map[int]jumpserver.AssetPage{0: {Next: "next-page", Results: firstPage[:2]}, 2: {Results: []jumpserver.Asset{wantAsset}}},
			wantOffsets: []int{0, 2},
		},
		{
			name: "not found after last page", reference: "web",
			pages:       map[int]jumpserver.AssetPage{0: {Count: 101, Results: firstPage}, 100: {Count: 101, Results: []jumpserver.Asset{{ID: "last", Name: "web-other"}}}},
			wantOffsets: []int{0, 100}, wantErr: ErrAssetNotFound,
		},
		{
			name: "later request failure propagates", reference: "web",
			pages:       map[int]jumpserver.AssetPage{0: {Count: 101, Results: firstPage}},
			wantOffsets: []int{0, 100}, wantErr: requestErr, failOffset: 100,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{detail: jumpserver.AssetDetail{Asset: wantAsset}}
			api.listAssets = func(query jumpserver.AssetQuery) (jumpserver.AssetPage, error) {
				if query.Search != tc.reference || query.Limit != 100 {
					t.Fatalf("query = %#v", query)
				}
				if tc.failOffset > 0 && query.Offset == tc.failOffset {
					return jumpserver.AssetPage{}, requestErr
				}
				page, ok := tc.pages[query.Offset]
				if !ok {
					t.Fatalf("unexpected page offset %d", query.Offset)
				}
				return page, nil
			}
			asset, err := ResolveAsset(context.Background(), api, tc.reference)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if asset.ID != wantAsset.ID || !reflect.DeepEqual(api.getAssetIDs, []string{wantAsset.ID}) {
					t.Fatalf("asset = %#v, detail IDs = %#v", asset, api.getAssetIDs)
				}
			} else if len(api.getAssetIDs) != 0 {
				t.Fatalf("unexpected detail request: %#v", api.getAssetIDs)
			}
			offsets := make([]int, len(api.listQueries))
			for i, query := range api.listQueries {
				offsets[i] = query.Offset
			}
			if !reflect.DeepEqual(offsets, tc.wantOffsets) {
				t.Fatalf("offsets = %v, want %v", offsets, tc.wantOffsets)
			}
		})
	}
}

func TestResolveAssetRejectsPaginationWithoutProgress(t *testing.T) {
	for _, results := range [][]jumpserver.Asset{nil, {{ID: "partial", Name: "web-backup"}}} {
		api := &fakeAPI{}
		api.listAssets = func(query jumpserver.AssetQuery) (jumpserver.AssetPage, error) {
			if len(api.listQueries) > 2 {
				t.Fatal("pagination did not stop")
			}
			return jumpserver.AssetPage{Count: 200, Next: "next-page", Results: results}, nil
		}
		_, err := ResolveAsset(context.Background(), api, "web")
		if err == nil || !strings.Contains(err.Error(), "pagination made no progress") {
			t.Fatalf("error = %v, want pagination failure", err)
		}
		if len(api.getAssetIDs) != 0 {
			t.Fatalf("unexpected detail request: %#v", api.getAssetIDs)
		}
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
		Config: staticConfig{value: configuration}, Tokens: staticTokens{token: credential.Token{AccessToken: "access", Site: "https://jump.example.test"}},
		NewAPI: func(string, string, string) (API, error) { return api, nil },
	}
	return service, api
}

func TestPrepareSFTPUsesItsOwnProtocolPermissionAndToken(t *testing.T) {
	service, api := testService()
	api.detail.Protocols = []jumpserver.Protocol{{Name: "sftp", Port: 22}}
	api.connection.Protocol = "sftp"
	prepared, err := service.Prepare(context.Background(), Options{Target: target.Input{Target: "asset-1"}, Protocol: "sftp"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Connection.Protocol != "sftp" || api.connectionRequest.Protocol != "sftp" || api.connectionRequest.ConnectMethod != "sftp_client" {
		t.Fatal("SFTP preparation did not use SFTP token")
	}
}

func TestPrepareRejectsUnknownProtocolBeforeResolvingResources(t *testing.T) {
	service, api := testService()
	api.detail.Protocols = []jumpserver.Protocol{{Name: "rdp"}}
	api.connection.Protocol = "rdp"
	_, err := service.Prepare(context.Background(), Options{Target: target.Input{Target: "asset-1"}, Protocol: "rdp"})
	if err == nil || !strings.Contains(err.Error(), "not supported") || len(api.listQueries) != 0 {
		t.Fatalf("error=%v resource requests=%d", err, len(api.listQueries))
	}
}

func TestPrepareRejectsConnectionProtocolMismatch(t *testing.T) {
	service, api := testService()
	api.detail.Protocols = []jumpserver.Protocol{{Name: "sftp"}}
	_, err := service.Prepare(context.Background(), Options{Target: target.Input{Target: "asset-1"}, Protocol: "sftp"})
	if err == nil {
		t.Fatal("accepted SSH credentials for an SFTP request")
	}
}

func TestPrepareSFTPDoesNotInferPermissionFromSSH(t *testing.T) {
	service, api := testService()
	_, err := service.Prepare(context.Background(), Options{Target: target.Input{Target: "asset-1"}, Protocol: "sftp"})
	if err == nil || api.connectionRequest.Asset != "" {
		t.Fatalf("error=%v; unauthorized SFTP token issued", err)
	}
}
