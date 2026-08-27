package resources

import (
	"context"
	"testing"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type staticConfig struct{ value projectconfig.Config }

func (s staticConfig) Load() (projectconfig.Config, error) { return s.value, nil }

type staticTokens struct{}

func (staticTokens) EnsureFresh(context.Context, string) (credential.Token, error) {
	return credential.Token{AccessToken: "access"}, nil
}

type fakeAPI struct {
	organizations []jumpserver.Organization
	page          jumpserver.AssetPage
	detail        jumpserver.AssetDetail
}

func (f fakeAPI) ListOrganizations(context.Context) ([]jumpserver.Organization, error) {
	return f.organizations, nil
}

func (f fakeAPI) ListAssets(context.Context, jumpserver.AssetQuery) (jumpserver.AssetPage, error) {
	return f.page, nil
}

func (f fakeAPI) GetAsset(context.Context, string) (jumpserver.AssetDetail, error) {
	return f.detail, nil
}

func TestServiceListsResourcesUsingSelectedProfileAndOrganization(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test", Organization: "org-default"}
	api := fakeAPI{
		organizations: []jumpserver.Organization{{ID: "org-1", Name: "One"}},
		page:          jumpserver.AssetPage{Results: []jumpserver.Asset{{ID: "asset-1", Name: "web"}}},
	}
	service := Service{
		Config: staticConfig{value: configuration}, Tokens: staticTokens{},
		NewAPI: func(site, accessToken, organization string) (API, error) {
			if site != "https://jump.example.test" || accessToken != "access" || (organization != "org-explicit" && organization != "") {
				t.Fatalf("factory args = %q %q %q", site, accessToken, organization)
			}
			return api, nil
		},
	}

	assets, err := service.ListAssets(context.Background(), "", "org-explicit", "web")
	if err != nil || len(assets.Results) != 1 {
		t.Fatalf("ListAssets = %#v, %v", assets, err)
	}
	organizations, err := service.ListOrganizations(context.Background(), "")
	if err != nil || len(organizations) != 1 {
		t.Fatalf("ListOrganizations = %#v, %v", organizations, err)
	}
}

func TestServiceFindAssetReturnsExactDetailForAccountDiscovery(t *testing.T) {
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test"}
	api := fakeAPI{
		page:   jumpserver.AssetPage{Results: []jumpserver.Asset{{ID: "asset-1", Name: "web"}}},
		detail: jumpserver.AssetDetail{Asset: jumpserver.Asset{ID: "asset-1", Name: "web"}, Accounts: []jumpserver.Account{{ID: "account-1", Username: "root"}}},
	}
	service := Service{Config: staticConfig{value: configuration}, Tokens: staticTokens{}, NewAPI: func(string, string, string) (API, error) { return api, nil }}

	asset, err := service.FindAsset(context.Background(), "", "", "web")
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID != "asset-1" || len(asset.Accounts) != 1 || asset.Accounts[0].Username != "root" {
		t.Fatalf("asset = %#v", asset)
	}
}
