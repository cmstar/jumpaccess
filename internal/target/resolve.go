package target

import (
	"fmt"
	"strings"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

type Input struct {
	Target       string
	Profile      string
	Organization string
	Account      string
}

type Selection struct {
	Profile      string
	SiteURL      string
	Organization string
	Asset        string
	Account      string
	Alias        string
}

func Resolve(cfg projectconfig.Config, input Input) (Selection, error) {
	if strings.TrimSpace(input.Target) == "" {
		return Selection{}, fmt.Errorf("target is required")
	}
	profileName := input.Profile
	if profileName == "" {
		profileName = cfg.CurrentProfile
	}
	profile, ok := cfg.Profiles[profileName]
	if !ok {
		return Selection{}, fmt.Errorf("profile %q does not exist", profileName)
	}

	alias, ok := profile.Aliases[input.Target]
	if !ok {
		organization := profile.Organization
		if input.Organization != "" {
			organization = input.Organization
		}
		return Selection{
			Profile:      profileName,
			SiteURL:      profile.URL,
			Organization: organization,
			Asset:        input.Target,
			Account:      input.Account,
		}, nil
	}
	organization := alias.Organization
	if organization == "" {
		organization = profile.Organization
	}
	if input.Organization != "" {
		organization = input.Organization
	}
	account := alias.Account
	if input.Account != "" {
		account = input.Account
	}

	return Selection{
		Profile:      profileName,
		SiteURL:      profile.URL,
		Organization: organization,
		Asset:        alias.Asset,
		Account:      account,
		Alias:        input.Target,
	}, nil
}
