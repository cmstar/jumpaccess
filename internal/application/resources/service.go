package resources

import (
	"context"
	"fmt"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

type ConfigLoader interface {
	Load() (projectconfig.Config, error)
}

type TokenManager interface {
	EnsureFresh(context.Context, string) (credential.Token, error)
}

type API interface {
	connectapp.AssetAPI
	ListOrganizations(context.Context) ([]jumpserver.Organization, error)
}

type Service struct {
	Config ConfigLoader
	Tokens TokenManager
	NewAPI func(site, accessToken, organization string) (API, error)
}

func (s Service) ListOrganizations(ctx context.Context, requestedProfile string) ([]jumpserver.Organization, error) {
	profile, configured, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return nil, err
	}
	api, err := s.client(ctx, profile, configured.URL, "")
	if err != nil {
		return nil, err
	}
	return api.ListOrganizations(ctx)
}

func (s Service) ListAssets(ctx context.Context, requestedProfile, organization, search string) (jumpserver.AssetPage, error) {
	profile, configured, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return jumpserver.AssetPage{}, err
	}
	if organization == "" {
		organization = configured.Organization
	}
	api, err := s.client(ctx, profile, configured.URL, organization)
	if err != nil {
		return jumpserver.AssetPage{}, err
	}
	return api.ListAssets(ctx, jumpserver.AssetQuery{Search: search, Limit: 100})
}

func (s Service) FindAsset(ctx context.Context, requestedProfile, organization, reference string) (jumpserver.AssetDetail, error) {
	profile, configured, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return jumpserver.AssetDetail{}, err
	}
	if organization == "" {
		organization = configured.Organization
	}
	api, err := s.client(ctx, profile, configured.URL, organization)
	if err != nil {
		return jumpserver.AssetDetail{}, err
	}
	return connectapp.ResolveAsset(ctx, api, reference)
}

func (s Service) resolveProfile(requested string) (string, projectconfig.Profile, error) {
	configuration, err := s.Config.Load()
	if err != nil {
		return "", projectconfig.Profile{}, err
	}
	profile := requested
	if profile == "" {
		profile = configuration.CurrentProfile
	}
	configured, ok := configuration.Profiles[profile]
	if !ok {
		return "", projectconfig.Profile{}, fmt.Errorf("profile %q does not exist", profile)
	}
	return profile, configured, nil
}

func (s Service) client(ctx context.Context, profile, site, organization string) (API, error) {
	token, err := s.Tokens.EnsureFresh(ctx, profile)
	if err != nil {
		return nil, err
	}
	if s.NewAPI == nil {
		return nil, fmt.Errorf("JumpServer API client is unavailable")
	}
	return s.NewAPI(site, token.AccessToken, organization)
}
