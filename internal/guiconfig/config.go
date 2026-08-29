package guiconfig

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 1

type Config struct {
	Version    int        `toml:"version"`
	Appearance Appearance `toml:"appearance"`
	Behavior   Behavior   `toml:"behavior"`
}

type Appearance struct {
	Theme              string `toml:"theme"`
	TerminalFontFamily string `toml:"terminal_font_family"`
	TerminalFontSize   int    `toml:"terminal_font_size"`
}

type Behavior struct {
	ConfirmCloseActiveSession bool `toml:"confirm_close_active_session"`
}

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Appearance: Appearance{
			Theme:              "system",
			TerminalFontFamily: "JetBrains Mono",
			TerminalFontSize:   13,
		},
		Behavior: Behavior{ConfirmCloseActiveSession: true},
	}
}

func Decode(data []byte) (Config, error) {
	result := Default()
	metadata, err := toml.Decode(string(data), &result)
	if err != nil {
		return Config{}, fmt.Errorf("decode GUI TOML: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("unknown GUI TOML field %q", undecoded[0].String())
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (c Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported GUI config version %d", c.Version)
	}
	switch c.Appearance.Theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("appearance.theme must be system, light, or dark")
	}
	font := strings.TrimSpace(c.Appearance.TerminalFontFamily)
	if font == "" || font != c.Appearance.TerminalFontFamily {
		return fmt.Errorf("appearance.terminal_font_family is invalid")
	}
	for _, character := range font {
		if unicode.IsControl(character) {
			return fmt.Errorf("appearance.terminal_font_family is invalid")
		}
	}
	if c.Appearance.TerminalFontSize < 9 || c.Appearance.TerminalFontSize > 32 {
		return fmt.Errorf("appearance.terminal_font_size must be between 9 and 32")
	}
	return nil
}
