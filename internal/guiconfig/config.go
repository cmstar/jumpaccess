package guiconfig

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 1

const (
	DefaultWindowWidth  = 1280
	DefaultWindowHeight = 800
	MinimumWindowWidth  = 960
	MinimumWindowHeight = 640
)

type Config struct {
	Version    int             `toml:"version"`
	Appearance Appearance      `toml:"appearance"`
	Behavior   Behavior        `toml:"behavior"`
	Window     WindowPlacement `toml:"window" json:"-"`
}

type Appearance struct {
	Theme              string `toml:"theme"`
	TerminalFontFamily string `toml:"terminal_font_family"`
	TerminalFontSize   int    `toml:"terminal_font_size"`
}

type Behavior struct {
	ConfirmCloseActiveSession bool `toml:"confirm_close_active_session"`
}

// WindowPlacement 保存桌面窗口最近一次退出时的状态。坐标和尺寸始终表示普通窗口状态，
// 避免最大化或最小化时的临时边界覆盖用户调整过的位置和大小。
type WindowPlacement struct {
	HasBounds bool `toml:"has_bounds"`
	Maximized bool `toml:"maximized"`
	X         int  `toml:"x"`
	Y         int  `toml:"y"`
	Width     int  `toml:"width"`
	Height    int  `toml:"height"`
}

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Appearance: Appearance{
			Theme:              "system",
			TerminalFontFamily: "JetBrains Mono",
			TerminalFontSize:   12,
		},
		Behavior: Behavior{ConfirmCloseActiveSession: true},
		Window: WindowPlacement{
			Width:  DefaultWindowWidth,
			Height: DefaultWindowHeight,
		},
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
	if c.Window.Width < MinimumWindowWidth || c.Window.Height < MinimumWindowHeight {
		return fmt.Errorf("window size must be at least %dx%d", MinimumWindowWidth, MinimumWindowHeight)
	}
	return nil
}
