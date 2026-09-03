package desktop

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	settingsapp "github.com/cmstar/jumpaccess/internal/application/settings"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type fakeAuth struct {
	statuses map[string]authapp.Status
}

func (f fakeAuth) Status(profile string) (authapp.Status, error) {
	return f.statuses[profile], nil
}

type fakeResources struct {
	organizations []jumpserver.Organization
	page          jumpserver.AssetPage
	details       map[string]jumpserver.AssetDetail
	lastSearch    string
}

func (f *fakeResources) ListOrganizations(context.Context, string) ([]jumpserver.Organization, error) {
	return f.organizations, nil
}

func (f *fakeResources) ListAssets(_ context.Context, _, _, search string, _, _ int) (jumpserver.AssetPage, error) {
	f.lastSearch = search
	return f.page, nil
}

func (f *fakeResources) FindAsset(_ context.Context, _, _, reference string) (jumpserver.AssetDetail, error) {
	return f.details[reference], nil
}

func TestBootstrapReturnsSortedProfilesAuthAndDesktopPreferences(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "production"
	configuration.Profiles["staging"] = projectconfig.Profile{URL: "https://staging.example.test", Aliases: map[string]projectconfig.Alias{}}
	configuration.Profiles["production"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-1",
		Aliases:      map[string]projectconfig.Alias{"web": {Asset: "asset-1"}},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	preferenceStore := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	preferences := guiconfig.Default()
	preferences.Appearance.Theme = "dark"
	preferences.Workspace = guiconfig.Workspace{ActiveTabID: "assets", Tabs: []guiconfig.WorkspaceTab{{ID: "assets", Type: "assets"}}}
	if err := preferenceStore.Save(preferences); err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:      store,
		Auth:        fakeAuth{statuses: map[string]authapp.Status{"production": {Profile: "production", LoggedIn: true, ExpiresAt: time.Unix(100, 0)}}},
		Preferences: preferenceStore,
	}

	got, err := service.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentProfile != "production" || got.CurrentOrganization != "org-1" || got.Preferences.Appearance.Theme != "dark" {
		t.Fatalf("Bootstrap = %#v", got)
	}
	if !reflect.DeepEqual(got.Workspace, preferences.Workspace) {
		t.Fatalf("workspace = %#v, want %#v", got.Workspace, preferences.Workspace)
	}
	if len(got.Profiles) != 2 || got.Profiles[0].Name != "production" || got.Profiles[1].Name != "staging" {
		t.Fatalf("profiles = %#v, want sorted production, staging", got.Profiles)
	}
	if !got.Profiles[0].Auth.LoggedIn || got.Profiles[0].AliasCount != 1 || got.Profiles[0].Auth.ExpiresAt != time.Unix(100, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("production summary = %#v", got.Profiles[0])
	}
}

func TestGetAuthStatusReturnsLatestCredentialState(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	service := Service{Auth: fakeAuth{statuses: map[string]authapp.Status{
		"production": {Profile: "production", LoggedIn: true, RefreshAvailable: true, ExpiresAt: expiresAt},
	}}}

	got, err := service.GetAuthStatus("production")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LoggedIn || !got.RefreshAvailable || got.ExpiresAt != expiresAt.UTC().Format(time.RFC3339) {
		t.Fatalf("GetAuthStatus = %#v", got)
	}
}

func TestListAssetsSearchesAliasesAndMergesWithoutDuplicates(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-1",
		Aliases: map[string]projectconfig.Alias{
			"production-web": {Asset: "asset-1", Organization: "org-1"},
			"database":       {Asset: "asset-2", Organization: "org-1", Account: "dba"},
		},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	resources := &fakeResources{
		page: jumpserver.AssetPage{Count: 1, Results: []jumpserver.Asset{{ID: "asset-1", Name: "web", Address: "10.0.0.1"}}},
		details: map[string]jumpserver.AssetDetail{
			"asset-1": {Asset: jumpserver.Asset{ID: "asset-1", Name: "web", Address: "10.0.0.1"}},
		},
	}
	service := Service{Config: store, Resources: resources}

	got, err := service.ListAssets(context.Background(), AssetListRequest{Search: "production", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if resources.lastSearch != "production" {
		t.Fatalf("remote search = %q", resources.lastSearch)
	}
	if got.Count != 1 || len(got.Results) != 1 || got.Results[0].ID != "asset-1" {
		t.Fatalf("asset page = %#v", got)
	}
	if len(got.Results[0].Aliases) != 1 || got.Results[0].Aliases[0].Name != "production-web" {
		t.Fatalf("asset aliases = %#v", got.Results[0].Aliases)
	}
	if got.AliasCount != 2 {
		t.Fatalf("AliasCount = %d, want organization total 2", got.AliasCount)
	}
}

func TestListAssetsIncludesConcreteOrganizationAliasesInAllOrganizations(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "00000000-0000-0000-0000-000000000000",
		Aliases: map[string]projectconfig.Alias{
			"production-web": {Asset: "asset-1", Organization: "org-2"},
		},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	resources := &fakeResources{
		page: jumpserver.AssetPage{Count: 1, Results: []jumpserver.Asset{{ID: "asset-1", Name: "web", Address: "10.0.0.1"}}},
	}
	service := Service{Config: store, Resources: resources}

	got, err := service.ListAssets(context.Background(), AssetListRequest{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if got.AliasCount != 1 {
		t.Fatalf("AliasCount = %d, want all organizations total 1", got.AliasCount)
	}
	if len(got.Results) != 1 || len(got.Results[0].Aliases) != 1 || got.Results[0].Aliases[0].Name != "production-web" {
		t.Fatalf("asset page = %#v, want alias from concrete organization", got)
	}
}

func TestListAssetsIncludesAllOrganizationsAliasInConcreteOrganization(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-2",
		Aliases: map[string]projectconfig.Alias{
			"production-web": {Asset: "asset-1", Organization: "00000000-0000-0000-0000-000000000000"},
		},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	resources := &fakeResources{
		page: jumpserver.AssetPage{Count: 1, Results: []jumpserver.Asset{{ID: "asset-1", Name: "web", Address: "10.0.0.1"}}},
	}
	service := Service{Config: store, Resources: resources}

	got, err := service.ListAssets(context.Background(), AssetListRequest{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if got.AliasCount != 1 || len(got.Results) != 1 || len(got.Results[0].Aliases) != 1 {
		t.Fatalf("asset page = %#v, want all-organizations alias in concrete organization", got)
	}
}

func TestCreateAliasDerivesOrganizationAndValidatesExistingAccount(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{URL: "https://jump.example.test", Organization: "org-1", Aliases: map[string]projectconfig.Alias{}}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	resources := &fakeResources{details: map[string]jumpserver.AssetDetail{
		"asset-1": {
			Asset:    jumpserver.Asset{ID: "asset-1", Name: "web"},
			Accounts: []jumpserver.Account{{ID: "account-1", Username: "deploy"}},
		},
	}}
	service := Service{Config: store, Resources: resources, Settings: settingsapp.Service{Store: store}}

	created, err := service.CreateAlias(context.Background(), CreateAliasRequest{Asset: "asset-1", Name: "production", Account: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Organization != "org-1" || created.Asset != "asset-1" || created.Account != "account-1" {
		t.Fatalf("created alias = %#v", created)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].Aliases["production"] != (projectconfig.Alias{Asset: "asset-1", Account: "account-1", Organization: "org-1"}) {
		t.Fatalf("stored alias = %#v", got.Profiles["work"].Aliases["production"])
	}
	if _, err := service.CreateAlias(context.Background(), CreateAliasRequest{Asset: "asset-1", Name: "invalid", Account: "missing"}); err == nil {
		t.Fatal("CreateAlias accepted an account not permitted by the asset")
	}
}

func TestRenameAliasPreservesTargetAccountAndOrganization(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL: "https://jump.example.test",
		Aliases: map[string]projectconfig.Alias{
			"production": {Asset: "asset-1", Account: "account-1", Organization: "org-1"},
		},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	service := Service{Config: store, Settings: settingsapp.Service{Store: store}}

	renamed, err := service.RenameAlias(RenameAliasRequest{CurrentName: "production", NewName: "primary"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed != (AliasView{Name: "primary", Asset: "asset-1", Account: "account-1", Organization: "org-1"}) {
		t.Fatalf("renamed Alias = %#v", renamed)
	}
	if _, err := service.RenameAlias(RenameAliasRequest{CurrentName: "primary", NewName: " primary "}); err == nil {
		t.Fatal("RenameAlias accepted a name with surrounding whitespace")
	}
}

func TestSetAliasAccountValidatesAgainstAliasesAsset(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-1",
		Aliases:      map[string]projectconfig.Alias{"production": {Asset: "asset-1", Organization: "org-1"}},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	resources := &fakeResources{details: map[string]jumpserver.AssetDetail{
		"asset-1": {Asset: jumpserver.Asset{ID: "asset-1"}, Accounts: []jumpserver.Account{{ID: "account-1", Username: "deploy"}}},
	}}
	service := Service{Config: store, Resources: resources, Settings: settingsapp.Service{Store: store}}

	if err := service.SetAliasAccount(context.Background(), AliasAccountRequest{Name: "production", Account: "deploy"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Profiles["work"].Aliases["production"].Account != "account-1" {
		t.Fatalf("stored account = %q", got.Profiles["work"].Aliases["production"].Account)
	}
}

func TestGetAssetReturnsPermittedAccountsProtocolsAndAliases(t *testing.T) {
	store := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	configuration.CurrentProfile = "work"
	configuration.Profiles["work"] = projectconfig.Profile{
		URL:          "https://jump.example.test",
		Organization: "org-1",
		Aliases:      map[string]projectconfig.Alias{"production": {Asset: "asset-1", Organization: "org-1"}},
	}
	if err := store.Save(configuration); err != nil {
		t.Fatal(err)
	}
	resources := &fakeResources{details: map[string]jumpserver.AssetDetail{
		"asset-1": {
			Asset:     jumpserver.Asset{ID: "asset-1", Name: "web"},
			Accounts:  []jumpserver.Account{{ID: "account-1", Username: "deploy"}},
			Protocols: []jumpserver.Protocol{{Name: "ssh", Port: 22}},
		},
	}}
	service := Service{Config: store, Resources: resources}

	got, err := service.GetAsset(context.Background(), AssetRequest{Asset: "asset-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "asset-1" || len(got.Accounts) != 1 || got.Accounts[0].Username != "deploy" {
		t.Fatalf("asset detail = %#v", got)
	}
	if len(got.Protocols) != 1 || got.Protocols[0].Name != "ssh" || len(got.Aliases) != 1 {
		t.Fatalf("asset detail metadata = %#v", got)
	}
}

func TestSavePreferencesWritesOnlyGUIStore(t *testing.T) {
	configStore := projectconfig.Store{Path: filepath.Join(t.TempDir(), "config.toml")}
	configuration := projectconfig.Default()
	if err := configStore.Save(configuration); err != nil {
		t.Fatal(err)
	}
	preferenceStore := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	service := Service{Config: configStore, Preferences: preferenceStore}
	want := guiconfig.Default()
	want.Appearance.Theme = "light"

	if err := service.SavePreferences(want); err != nil {
		t.Fatal(err)
	}
	got, err := preferenceStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preferences = %#v, want %#v", got, want)
	}
}

func TestSavePreferencesPreservesWindowPlacement(t *testing.T) {
	preferenceStore := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	stored := guiconfig.Default()
	stored.Window = guiconfig.WindowPlacement{HasBounds: true, Maximized: true, X: 120, Y: 90, Width: 1440, Height: 900}
	stored.Workspace = guiconfig.Workspace{ActiveTabID: "assets", Tabs: []guiconfig.WorkspaceTab{{ID: "assets", Type: "assets"}}}
	if err := preferenceStore.Save(stored); err != nil {
		t.Fatal(err)
	}
	service := Service{Preferences: preferenceStore}
	request := guiconfig.Default()
	request.Appearance.Theme = "dark"

	if err := service.SavePreferences(request); err != nil {
		t.Fatal(err)
	}
	got, err := preferenceStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Window != stored.Window {
		t.Fatalf("window placement = %#v, want %#v", got.Window, stored.Window)
	}
	if got.Appearance.Theme != "dark" {
		t.Fatalf("theme = %q, want dark", got.Appearance.Theme)
	}
	if !reflect.DeepEqual(got.Workspace, stored.Workspace) {
		t.Fatalf("workspace = %#v, want %#v", got.Workspace, stored.Workspace)
	}
}

func TestSaveWorkspacePreservesPreferencesAndWindowPlacement(t *testing.T) {
	preferenceStore := guiconfig.Store{Path: filepath.Join(t.TempDir(), "gui.toml")}
	stored := guiconfig.Default()
	stored.Appearance.Theme = "dark"
	stored.Tabs.ConfirmCloseActiveSession = false
	stored.Window = guiconfig.WindowPlacement{HasBounds: true, X: 120, Y: 90, Width: 1440, Height: 900}
	if err := preferenceStore.Save(stored); err != nil {
		t.Fatal(err)
	}
	service := Service{Preferences: preferenceStore}
	want := guiconfig.Workspace{ActiveTabID: "ssh-1", Tabs: []guiconfig.WorkspaceTab{{
		ID: "ssh-1", Type: "ssh", Profile: "production", Organization: "org-1", Target: "asset-1", Account: "account-1", AssetID: "asset-1", AssetName: "web-01",
	}}}

	if err := service.SaveWorkspace(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := preferenceStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Workspace, want) {
		t.Fatalf("workspace = %#v, want %#v", got.Workspace, want)
	}
	if got.Appearance != stored.Appearance || got.Terminal != stored.Terminal || got.Tabs != stored.Tabs || got.Window != stored.Window {
		t.Fatalf("unrelated preferences changed: got %#v, want %#v", got, stored)
	}
}
