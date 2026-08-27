package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/credential"
)

type ConfigLoader interface {
	Load() (projectconfig.Config, error)
}

type Status struct {
	Profile          string
	LoggedIn         bool
	Expired          bool
	RefreshAvailable bool
	ExpiresAt        time.Time
}

type Service struct {
	Config    ConfigLoader
	Tokens    TokenRepository
	Manager   Manager
	LoginFlow func(context.Context, string) (credential.Token, error)
	Revoke    func(context.Context, credential.Token) error
	Now       func() time.Time
	Timeout   time.Duration
}

func (s Service) Login(ctx context.Context, requestedProfile string) (Status, error) {
	profile, configured, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return Status{}, err
	}
	if s.LoginFlow == nil {
		return Status{}, fmt.Errorf("browser login is unavailable")
	}
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	token, err := s.LoginFlow(ctx, configured.URL)
	if err != nil {
		return Status{}, err
	}
	if err := s.Tokens.Save(profile, token); err != nil {
		return Status{}, fmt.Errorf("save OAuth credential: %w", err)
	}
	return s.statusFor(profile, token), nil
}

func (s Service) Status(requestedProfile string) (Status, error) {
	profile, _, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return Status{}, err
	}
	token, err := s.Tokens.Load(profile)
	if errors.Is(err, credential.ErrNotFound) {
		return Status{Profile: profile}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("load OAuth credential: %w", err)
	}
	return s.statusFor(profile, token), nil
}

func (s Service) Refresh(ctx context.Context, requestedProfile string) (Status, error) {
	profile, _, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return Status{}, err
	}
	token, err := s.Manager.RefreshNow(ctx, profile)
	if err != nil {
		return Status{}, err
	}
	return s.statusFor(profile, token), nil
}

func (s Service) Logout(ctx context.Context, requestedProfile string) error {
	profile, _, err := s.resolveProfile(requestedProfile)
	if err != nil {
		return err
	}
	token, err := s.Tokens.Load(profile)
	if errors.Is(err, credential.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load OAuth credential: %w", err)
	}
	if s.Revoke != nil {
		if err := s.Revoke(ctx, token); err != nil {
			return err
		}
	}
	if err := s.Tokens.Delete(profile); err != nil && !errors.Is(err, credential.ErrNotFound) {
		return fmt.Errorf("delete OAuth credential: %w", err)
	}
	return nil
}

func (s Service) resolveProfile(requested string) (string, projectconfig.Profile, error) {
	value, err := s.Config.Load()
	if err != nil {
		return "", projectconfig.Profile{}, err
	}
	profile := requested
	if profile == "" {
		profile = value.CurrentProfile
	}
	if profile == "" {
		return "", projectconfig.Profile{}, fmt.Errorf("no current profile; add one with jumpctl profile add")
	}
	configured, ok := value.Profiles[profile]
	if !ok {
		return "", projectconfig.Profile{}, fmt.Errorf("profile %q does not exist", profile)
	}
	return profile, configured, nil
}

func (s Service) statusFor(profile string, token credential.Token) Status {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return Status{
		Profile:          profile,
		LoggedIn:         true,
		Expired:          !token.ExpiresAt.After(now),
		RefreshAvailable: token.RefreshToken != "",
		ExpiresAt:        token.ExpiresAt,
	}
}
