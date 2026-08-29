package settings

import (
	"context"
	"errors"
	"fmt"
	"sort"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

type CredentialRemover interface {
	Delete(string) error
}

type Service struct {
	Store       projectconfig.Store
	Credentials CredentialRemover
}

func (s Service) AddProfile(name, siteURL string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
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
		return nil
	})
}

func (s Service) UseProfile(name string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		value.CurrentProfile = name
		return nil
	})
}

func (s Service) DeleteProfile(name string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		if _, exists := value.Profiles[name]; !exists {
			return fmt.Errorf("profile %q does not exist", name)
		}
		if s.Credentials != nil {
			if err := s.Credentials.Delete(name); err != nil && !errors.Is(err, credential.ErrNotFound) {
				return fmt.Errorf("delete OAuth credential for profile %q: %w", name, err)
			}
		}
		delete(value.Profiles, name)
		if value.CurrentProfile == name {
			names := make([]string, 0, len(value.Profiles))
			for profileName := range value.Profiles {
				names = append(names, profileName)
			}
			sort.Strings(names)
			value.CurrentProfile = ""
			if len(names) > 0 {
				value.CurrentProfile = names[0]
			}
		}
		return nil
	})
}

func (s Service) SetAlias(profileName, name string, alias projectconfig.Alias) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		resolvedName, profile, err := resolveProfile(*value, profileName)
		if err != nil {
			return err
		}
		if profile.Aliases == nil {
			profile.Aliases = make(map[string]projectconfig.Alias)
		}
		profile.Aliases[name] = alias
		value.Profiles[resolvedName] = profile
		return nil
	})
}

func (s Service) SetProfileOrganization(profileName, organization string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		resolvedName, profile, err := resolveProfile(*value, profileName)
		if err != nil {
			return err
		}
		profile.Organization = organization
		value.Profiles[resolvedName] = profile
		return nil
	})
}

func (s Service) DeleteAlias(profileName, name string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		resolvedName, profile, err := resolveProfile(*value, profileName)
		if err != nil {
			return err
		}
		if _, exists := profile.Aliases[name]; !exists {
			return fmt.Errorf("alias %q does not exist in profile %q", name, resolvedName)
		}
		delete(profile.Aliases, name)
		value.Profiles[resolvedName] = profile
		return nil
	})
}

func (s Service) SetAliasAccount(profileName, name, account string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		resolvedName, profile, err := resolveProfile(*value, profileName)
		if err != nil {
			return err
		}
		alias, exists := profile.Aliases[name]
		if !exists {
			return fmt.Errorf("alias %q does not exist in profile %q", name, resolvedName)
		}
		alias.Account = account
		profile.Aliases[name] = alias
		value.Profiles[resolvedName] = profile
		return nil
	})
}

func resolveProfile(value projectconfig.Config, requested string) (string, projectconfig.Profile, error) {
	profileName := requested
	if profileName == "" {
		profileName = value.CurrentProfile
	}
	profile, ok := value.Profiles[profileName]
	if !ok {
		return "", projectconfig.Profile{}, fmt.Errorf("profile %q does not exist", profileName)
	}
	return profileName, profile, nil
}
