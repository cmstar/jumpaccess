package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 1

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	value, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = value
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

type Config struct {
	Version        int                `toml:"version"`
	CurrentProfile string             `toml:"current_profile"`
	Behavior       Behavior           `toml:"behavior"`
	Profiles       map[string]Profile `toml:"profiles"`
}

type Behavior struct {
	RefreshCheckInterval Duration `toml:"refresh_check_interval"`
	RefreshBeforeExpiry  Duration `toml:"refresh_before_expiry"`
	ConnectTimeout       Duration `toml:"connect_timeout"`
	OAuthTimeout         Duration `toml:"oauth_timeout"`
}

type Profile struct {
	URL          string           `toml:"url"`
	Organization string           `toml:"organization,omitempty"`
	Aliases      map[string]Alias `toml:"aliases,omitempty"`
}

type Alias struct {
	Asset        string `toml:"asset"`
	Account      string `toml:"account,omitempty"`
	Organization string `toml:"organization,omitempty"`
}

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Behavior: Behavior{
			RefreshCheckInterval: Duration{Duration: 30 * time.Second},
			RefreshBeforeExpiry:  Duration{Duration: time.Minute},
			ConnectTimeout:       Duration{Duration: 30 * time.Second},
			OAuthTimeout:         Duration{Duration: 5 * time.Minute},
		},
		Profiles: make(map[string]Profile),
	}
}

func Decode(data []byte) (Config, error) {
	result := Default()
	metadata, err := toml.Decode(string(data), &result)
	if err != nil {
		return Config{}, fmt.Errorf("decode TOML: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("unknown TOML field %q", undecoded[0].String())
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (c *Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	if c.CurrentProfile != "" {
		if _, ok := c.Profiles[c.CurrentProfile]; !ok {
			return fmt.Errorf("current profile %q does not exist", c.CurrentProfile)
		}
	}

	for name, profile := range c.Profiles {
		if !validProfileName(name) {
			return fmt.Errorf("profile name %q is invalid", name)
		}
		normalizedURL, err := NormalizeProfileURL(profile.URL)
		if err != nil {
			return fmt.Errorf("profile %q has invalid URL", name)
		}
		profile.URL = normalizedURL
		if profile.Aliases == nil {
			profile.Aliases = make(map[string]Alias)
		}
		for aliasName, alias := range profile.Aliases {
			if strings.TrimSpace(alias.Asset) == "" {
				return fmt.Errorf("alias %q in profile %q has no asset", aliasName, name)
			}
		}
		c.Profiles[name] = profile
	}
	for name, value := range map[string]time.Duration{
		"refresh_check_interval": c.Behavior.RefreshCheckInterval.Duration,
		"refresh_before_expiry":  c.Behavior.RefreshBeforeExpiry.Duration,
		"connect_timeout":        c.Behavior.ConnectTimeout.Duration,
		"oauth_timeout":          c.Behavior.OAuthTimeout.Duration,
	} {
		if value <= 0 {
			return fmt.Errorf("behavior.%s must be positive", name)
		}
	}

	return nil
}

func NormalizeProfileURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid profile URL")
	}
	return strings.TrimRight(value, "/"), nil
}

func validProfileName(name string) bool {
	if !utf8.ValidString(name) || strings.TrimSpace(name) != name || name == "" || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
