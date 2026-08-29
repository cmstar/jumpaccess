package desktop

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/guiconfig"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type ConfigLoader interface {
	Load() (projectconfig.Config, error)
}

type AuthService interface {
	Status(string) (authapp.Status, error)
}

type ResourceService interface {
	ListOrganizations(context.Context, string) ([]jumpserver.Organization, error)
	ListAssets(context.Context, string, string, string, int, int) (jumpserver.AssetPage, error)
	FindAsset(context.Context, string, string, string) (jumpserver.AssetDetail, error)
}

type SettingsService interface {
	AddProfile(string, string) error
	UseProfile(string) error
	SetProfileOrganization(string, string) error
	SetAlias(string, string, projectconfig.Alias) error
	DeleteAlias(string, string) error
	SetAliasAccount(string, string, string) error
}

type PreferenceStore interface {
	Load() (guiconfig.Config, error)
	Save(guiconfig.Config) error
}

type Service struct {
	Version     string
	Licenses    string
	Login       *LoginCoordinator
	Config      ConfigLoader
	Auth        AuthService
	Resources   ResourceService
	Settings    SettingsService
	Preferences PreferenceStore
}

func (s Service) StartLogin(ctx context.Context, profile string) (LoginAttempt, error) {
	if s.Login == nil {
		return LoginAttempt{}, fmt.Errorf("OAuth login is unavailable")
	}
	return s.Login.Start(ctx, profile)
}

func (s Service) CompleteLogin(ctx context.Context, attemptID, callbackURL string) (AuthStatus, error) {
	if s.Login == nil {
		return AuthStatus{}, fmt.Errorf("OAuth login is unavailable")
	}
	return s.Login.Complete(ctx, attemptID, callbackURL)
}

func (s Service) CancelLogin(attemptID string) error {
	if s.Login == nil {
		return fmt.Errorf("OAuth login is unavailable")
	}
	return s.Login.Cancel(attemptID)
}

func (s Service) Bootstrap() (BootstrapState, error) {
	configuration, err := s.Config.Load()
	if err != nil {
		return BootstrapState{}, err
	}
	preferences := guiconfig.Default()
	if s.Preferences != nil {
		preferences, err = s.Preferences.Load()
		if err != nil {
			return BootstrapState{}, err
		}
	}
	names := make([]string, 0, len(configuration.Profiles))
	for name := range configuration.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]ProfileSummary, 0, len(names))
	for _, name := range names {
		profile := configuration.Profiles[name]
		summary := ProfileSummary{
			Name:         name,
			URL:          profile.URL,
			Organization: profile.Organization,
			AliasCount:   len(profile.Aliases),
		}
		if s.Auth != nil {
			status, statusErr := s.Auth.Status(name)
			if statusErr != nil {
				return BootstrapState{}, statusErr
			}
			summary.Auth = authStatus(status)
		}
		profiles = append(profiles, summary)
	}
	currentOrganization := ""
	if profile, ok := configuration.Profiles[configuration.CurrentProfile]; ok {
		currentOrganization = profile.Organization
	}
	return BootstrapState{
		Version:             s.Version,
		CurrentProfile:      configuration.CurrentProfile,
		CurrentOrganization: currentOrganization,
		Profiles:            profiles,
		Preferences:         preferences,
	}, nil
}

func (s Service) ListOrganizations(ctx context.Context, profile string) ([]OrganizationView, error) {
	organizations, err := s.Resources.ListOrganizations(ctx, profile)
	if err != nil {
		return nil, err
	}
	result := make([]OrganizationView, 0, len(organizations))
	for _, organization := range organizations {
		result = append(result, OrganizationView{ID: organization.ID, Name: organization.Name})
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})
	return result, nil
}

func (s Service) ListAssets(ctx context.Context, request AssetListRequest) (AssetPage, error) {
	profileName, profile, organization, err := s.resolveContext(request.Profile, request.Organization)
	if err != nil {
		return AssetPage{}, err
	}
	page, err := s.Resources.ListAssets(ctx, profileName, organization, request.Search, request.Offset, request.Limit)
	if err != nil {
		return AssetPage{}, err
	}
	assets := make([]jumpserver.Asset, 0, len(page.Results))
	assets = append(assets, page.Results...)
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		seen[asset.ID] = struct{}{}
	}
	query := strings.TrimSpace(request.Search)
	if query != "" {
		for name, alias := range profile.Aliases {
			if !strings.Contains(strings.ToLower(name), strings.ToLower(query)) || !sameOrganization(alias.Organization, organization) {
				continue
			}
			detail, findErr := s.Resources.FindAsset(ctx, profileName, organizationForAlias(alias, organization), alias.Asset)
			if findErr != nil {
				return AssetPage{}, findErr
			}
			if _, exists := seen[detail.ID]; exists {
				continue
			}
			seen[detail.ID] = struct{}{}
			assets = append(assets, detail.Asset)
		}
	}
	results := make([]AssetView, 0, len(assets))
	aliasCount := 0
	for _, asset := range assets {
		aliases := aliasesForAsset(profile.Aliases, organization, asset)
		aliasCount += len(aliases)
		results = append(results, assetView(asset, aliases))
	}
	count := page.Count
	if minimum := request.Offset + len(results); count < minimum {
		count = minimum
	}
	return AssetPage{
		Count:      count,
		Offset:     request.Offset,
		Limit:      request.Limit,
		AliasCount: aliasCount,
		Results:    results,
	}, nil
}

func (s Service) CreateAlias(ctx context.Context, request CreateAliasRequest) (AliasView, error) {
	profileName, profile, organization, err := s.resolveContext(request.Profile, "")
	if err != nil {
		return AliasView{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || name != request.Name {
		return AliasView{}, fmt.Errorf("alias name is invalid")
	}
	if _, exists := profile.Aliases[name]; exists {
		return AliasView{}, fmt.Errorf("alias %q already exists in profile %q", name, profileName)
	}
	detail, err := s.Resources.FindAsset(ctx, profileName, organization, request.Asset)
	if err != nil {
		return AliasView{}, err
	}
	account, err := resolveAccount(detail.Accounts, request.Account)
	if err != nil {
		return AliasView{}, err
	}
	alias := projectconfig.Alias{Asset: detail.ID, Account: account, Organization: organization}
	if err := s.Settings.SetAlias(profileName, name, alias); err != nil {
		return AliasView{}, err
	}
	return AliasView{Name: name, Asset: alias.Asset, Account: alias.Account, Organization: alias.Organization}, nil
}

func (s Service) GetAsset(ctx context.Context, request AssetRequest) (AssetDetailView, error) {
	profileName, profile, organization, err := s.resolveContext(request.Profile, request.Organization)
	if err != nil {
		return AssetDetailView{}, err
	}
	detail, err := s.Resources.FindAsset(ctx, profileName, organization, request.Asset)
	if err != nil {
		return AssetDetailView{}, err
	}
	result := AssetDetailView{
		AssetView: assetView(detail.Asset, aliasesForAsset(profile.Aliases, organization, detail.Asset)),
		Accounts:  make([]AccountView, 0, len(detail.Accounts)),
		Protocols: make([]ProtocolView, 0, len(detail.Protocols)),
	}
	for _, account := range detail.Accounts {
		result.Accounts = append(result.Accounts, AccountView{ID: account.ID, Name: account.Name, Alias: account.Alias, Username: account.Username})
	}
	for _, protocol := range detail.Protocols {
		result.Protocols = append(result.Protocols, ProtocolView{Name: protocol.Name, Port: protocol.Port})
	}
	return result, nil
}

func (s Service) QuickSearch(ctx context.Context, request QuickSearchRequest) ([]AssetView, error) {
	limit := request.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page, err := s.ListAssets(ctx, AssetListRequest{
		Profile:      request.Profile,
		Organization: request.Organization,
		Search:       request.Query,
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}
	return page.Results, nil
}

func (s Service) AddProfile(name, siteURL string) error {
	return s.Settings.AddProfile(name, siteURL)
}

func (s Service) UseProfile(name string) error {
	return s.Settings.UseProfile(name)
}

func (s Service) SetOrganization(profile, organization string) error {
	return s.Settings.SetProfileOrganization(profile, organization)
}

func (s Service) DeleteAlias(profile, name string) error {
	return s.Settings.DeleteAlias(profile, name)
}

func (s Service) SavePreferences(value guiconfig.Config) error {
	if s.Preferences == nil {
		return fmt.Errorf("GUI preference store is unavailable")
	}
	return s.Preferences.Save(value)
}

func (s Service) LicenseText() string {
	return s.Licenses
}

func (s Service) RefreshAuth(ctx context.Context, profile string) (AuthStatus, error) {
	refresher, ok := s.Auth.(interface {
		Refresh(context.Context, string) (authapp.Status, error)
	})
	if !ok {
		return AuthStatus{}, fmt.Errorf("OAuth refresh is unavailable")
	}
	status, err := refresher.Refresh(ctx, profile)
	if err != nil {
		return AuthStatus{}, err
	}
	return authStatus(status), nil
}

func (s Service) Logout(ctx context.Context, profile string) error {
	logout, ok := s.Auth.(interface {
		Logout(context.Context, string) error
	})
	if !ok {
		return fmt.Errorf("OAuth logout is unavailable")
	}
	return logout.Logout(ctx, profile)
}

func (s Service) SetAliasAccount(ctx context.Context, request AliasAccountRequest) error {
	profileName, profile, _, err := s.resolveContext(request.Profile, "")
	if err != nil {
		return err
	}
	alias, exists := profile.Aliases[request.Name]
	if !exists {
		return fmt.Errorf("alias %q does not exist in profile %q", request.Name, profileName)
	}
	organization := organizationForAlias(alias, profile.Organization)
	detail, err := s.Resources.FindAsset(ctx, profileName, organization, alias.Asset)
	if err != nil {
		return err
	}
	account, err := resolveAccount(detail.Accounts, request.Account)
	if err != nil {
		return err
	}
	return s.Settings.SetAliasAccount(profileName, request.Name, account)
}

func (s Service) resolveContext(requestedProfile, requestedOrganization string) (string, projectconfig.Profile, string, error) {
	configuration, err := s.Config.Load()
	if err != nil {
		return "", projectconfig.Profile{}, "", err
	}
	profileName := requestedProfile
	if profileName == "" {
		profileName = configuration.CurrentProfile
	}
	profile, exists := configuration.Profiles[profileName]
	if !exists {
		return "", projectconfig.Profile{}, "", fmt.Errorf("profile %q does not exist", profileName)
	}
	organization := requestedOrganization
	if organization == "" {
		organization = profile.Organization
	}
	return profileName, profile, organization, nil
}

func aliasesForAsset(aliases map[string]projectconfig.Alias, organization string, asset jumpserver.Asset) []AliasView {
	names := make([]string, 0)
	for name, alias := range aliases {
		if sameOrganization(alias.Organization, organization) && aliasMatchesAsset(alias, asset) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := make([]AliasView, 0, len(names))
	for _, name := range names {
		alias := aliases[name]
		result = append(result, AliasView{Name: name, Asset: alias.Asset, Account: alias.Account, Organization: alias.Organization})
	}
	return result
}

func aliasMatchesAsset(alias projectconfig.Alias, asset jumpserver.Asset) bool {
	return alias.Asset == asset.ID || strings.EqualFold(alias.Asset, asset.Name) || strings.EqualFold(alias.Asset, asset.Address)
}

func sameOrganization(aliasOrganization, selectedOrganization string) bool {
	return aliasOrganization == "" || aliasOrganization == selectedOrganization
}

func organizationForAlias(alias projectconfig.Alias, fallback string) string {
	if alias.Organization != "" {
		return alias.Organization
	}
	return fallback
}

func assetView(asset jumpserver.Asset, aliases []AliasView) AssetView {
	return AssetView{
		ID:       asset.ID,
		Name:     asset.Name,
		Address:  asset.Address,
		Type:     asset.Type.Label,
		Category: asset.Category.Label,
		Aliases:  aliases,
	}
}

func authStatus(status authapp.Status) AuthStatus {
	expiresAt := ""
	if !status.ExpiresAt.IsZero() {
		expiresAt = status.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return AuthStatus{
		LoggedIn:         status.LoggedIn,
		Expired:          status.Expired,
		RefreshAvailable: status.RefreshAvailable,
		ExpiresAt:        expiresAt,
	}
}

func resolveAccount(accounts []jumpserver.Account, reference string) (string, error) {
	if reference == "" {
		return "", nil
	}
	for _, account := range accounts {
		if account.ID == reference || strings.EqualFold(account.Name, reference) || strings.EqualFold(account.Alias, reference) || strings.EqualFold(account.Username, reference) {
			if account.ID != "" {
				return account.ID, nil
			}
			return account.Username, nil
		}
	}
	return "", fmt.Errorf("account %q is not permitted by asset", reference)
}
