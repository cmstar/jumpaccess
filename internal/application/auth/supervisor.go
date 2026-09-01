package auth

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/cmstar/jumpaccess/internal/credential"
)

type CredentialLoader interface {
	Load(profile string) (credential.Token, error)
}

type TokenFreshener interface {
	EnsureFresh(context.Context, string) (credential.Token, error)
}

// ProfileSupervisor 在应用存活期间持续检查所有已保存 Refresh Token 的
// Profile。它每轮重新读取配置和凭据，因此运行期间新增、删除或重新登录的
// Profile 不需要重启应用即可生效。
type ProfileSupervisor struct {
	Config      ConfigLoader
	Credentials CredentialLoader
	Freshener   TokenFreshener
	Interval    time.Duration
	OnError     func(profile string, err error)
}

type credentialRevision struct {
	ExpiresAt   time.Time
	RefreshedAt time.Time
}

func (s ProfileSupervisor) Run(ctx context.Context) {
	if s.Config == nil || s.Credentials == nil || s.Freshener == nil || s.Interval <= 0 {
		return
	}

	loginRequired := make(map[string]credentialRevision)
	s.check(ctx, loginRequired)
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx, loginRequired)
		}
	}
}

func (s ProfileSupervisor) check(ctx context.Context, loginRequired map[string]credentialRevision) {
	if ctx.Err() != nil {
		return
	}
	configuration, err := s.Config.Load()
	if err != nil {
		s.report("", err)
		return
	}

	profiles := make([]string, 0, len(configuration.Profiles))
	active := make(map[string]struct{}, len(configuration.Profiles))
	for profile := range configuration.Profiles {
		profiles = append(profiles, profile)
		active[profile] = struct{}{}
	}
	sort.Strings(profiles)
	for profile := range loginRequired {
		if _, ok := active[profile]; !ok {
			delete(loginRequired, profile)
		}
	}

	for _, profile := range profiles {
		if ctx.Err() != nil {
			return
		}
		token, err := s.Credentials.Load(profile)
		if errors.Is(err, credential.ErrNotFound) {
			delete(loginRequired, profile)
			continue
		}
		if err != nil {
			s.report(profile, err)
			continue
		}
		if token.RefreshToken == "" {
			delete(loginRequired, profile)
			continue
		}

		revision := credentialRevision{
			ExpiresAt:   token.ExpiresAt,
			RefreshedAt: token.RefreshedAt,
		}
		if blockedRevision, ok := loginRequired[profile]; ok && blockedRevision == revision {
			continue
		}
		if _, err := s.Freshener.EnsureFresh(ctx, profile); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			if errors.Is(err, ErrLoginRequired) {
				loginRequired[profile] = revision
			}
			s.report(profile, err)
			continue
		}
		delete(loginRequired, profile)
	}
}

func (s ProfileSupervisor) report(profile string, err error) {
	if s.OnError != nil {
		s.OnError(profile, err)
	}
}
