package settings

import (
	"fmt"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
)

type Service struct {
	Store projectconfig.Store
}

func (s Service) AddProfile(name, siteURL string) error {
	value, err := s.Store.Load()
	if err != nil {
		return err
	}
	if _, exists := value.Profiles[name]; exists {
		return fmt.Errorf("profile %q already exists", name)
	}
	value.Profiles[name] = projectconfig.Profile{
		URL:     siteURL,
		Aliases: make(map[string]projectconfig.Alias),
	}
	if value.CurrentProfile == "" {
		value.CurrentProfile = name
	}
	return s.Store.Save(value)
}

func (s Service) UseProfile(name string) error {
	value, err := s.Store.Load()
	if err != nil {
		return err
	}
	value.CurrentProfile = name
	return s.Store.Save(value)
}

func (s Service) SetAlias(profileName, name string, alias projectconfig.Alias) error {
	value, err := s.Store.Load()
	if err != nil {
		return err
	}
	if profileName == "" {
		profileName = value.CurrentProfile
	}
	profile, ok := value.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q does not exist", profileName)
	}
	if profile.Aliases == nil {
		profile.Aliases = make(map[string]projectconfig.Alias)
	}
	profile.Aliases[name] = alias
	value.Profiles[profileName] = profile
	return s.Store.Save(value)
}
