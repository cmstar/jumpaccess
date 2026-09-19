package settings

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
	"github.com/cmstar/jumpaccess/internal/filelock"
)

type CredentialRemover interface {
	Delete(string) error
}

type Service struct {
	Store       projectconfig.Store
	Credentials CredentialRemover
	Locker      authapp.Locker
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

func (s Service) UpdateProfileURL(name, siteURL string) error {
	normalizedURL, err := projectconfig.NormalizeProfileURL(siteURL)
	if err != nil {
		return fmt.Errorf("profile %q has invalid URL", name)
	}
	unlock, err := s.lockProfile(name)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		profile, exists := value.Profiles[name]
		if !exists {
			return fmt.Errorf("profile %q does not exist", name)
		}
		if profile.URL == normalizedURL {
			return nil
		}
		if s.Credentials != nil {
			if err := s.Credentials.Delete(name); err != nil && !errors.Is(err, credential.ErrNotFound) {
				return fmt.Errorf("delete OAuth credential for profile %q: %w", name, err)
			}
		}
		profile.URL = normalizedURL
		value.Profiles[name] = profile
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
	unlock, err := s.lockProfile(name)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
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

func (s Service) lockProfile(name string) (func() error, error) {
	locker := s.Locker
	if locker == nil {
		locker = filelock.Locker{Dir: filepath.Join(filepath.Dir(s.Store.Path), "locks")}
	}
	return authapp.LockProfile(context.Background(), locker, name)
}

func (s Service) SetAlias(profileName, name string, alias projectconfig.Alias) error {
	return s.saveAlias(profileName, name, alias, true)
}

// CreateAlias 在同一配置事务内检查名称与写入，防止并发创建覆盖已有 Alias。
func (s Service) CreateAlias(profileName, name string, alias projectconfig.Alias) error {
	return s.saveAlias(profileName, name, alias, false)
}

func (s Service) saveAlias(profileName, name string, alias projectconfig.Alias, replace bool) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		resolvedName, profile, err := resolveProfile(*value, profileName)
		if err != nil {
			return err
		}
		if _, exists := profile.Aliases[name]; exists && !replace {
			return fmt.Errorf("alias %q already exists in profile %q", name, resolvedName)
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

func (s Service) RenameAlias(profileName, currentName, newName string) error {
	return s.Store.Update(context.Background(), func(value *projectconfig.Config) error {
		resolvedName, profile, err := resolveProfile(*value, profileName)
		if err != nil {
			return err
		}
		alias, exists := profile.Aliases[currentName]
		if !exists {
			return fmt.Errorf("alias %q does not exist in profile %q", currentName, resolvedName)
		}
		if currentName == newName {
			return nil
		}
		if _, exists := profile.Aliases[newName]; exists {
			return fmt.Errorf("alias %q already exists in profile %q", newName, resolvedName)
		}
		profile.Aliases[newName] = alias
		delete(profile.Aliases, currentName)
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
