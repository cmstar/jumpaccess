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
	Workspace  Workspace       `toml:"workspace" json:"-"`
	Window     WindowPlacement `toml:"window" json:"-"`
}

type Appearance struct {
	Theme              string `toml:"theme"`
	TerminalFontFamily string `toml:"terminal_font_family"`
	TerminalFontSize   int    `toml:"terminal_font_size"`
}

type Behavior struct {
	ConfirmCloseActiveSession bool `toml:"confirm_close_active_session"`
	ShowTabCloseButtons       bool `toml:"show_tab_close_buttons"`
}

// Workspace 保存桌面工作区的稳定 Tab 描述。SSH Tab 只保存重连所需的目标信息，
// 不保存进程内 Session ID、终端输出或连接状态。
type Workspace struct {
	ActiveTabID string         `toml:"active_tab_id" json:"activeTabId"`
	Tabs        []WorkspaceTab `toml:"tabs" json:"tabs"`
}

type WorkspaceTab struct {
	ID           string `toml:"id" json:"id"`
	Type         string `toml:"type" json:"type"`
	Profile      string `toml:"profile,omitempty" json:"profile,omitempty"`
	Organization string `toml:"organization,omitempty" json:"organization,omitempty"`
	Target       string `toml:"target,omitempty" json:"target,omitempty"`
	Account      string `toml:"account,omitempty" json:"account,omitempty"`
	AssetID      string `toml:"asset_id,omitempty" json:"assetId,omitempty"`
	AssetName    string `toml:"asset_name,omitempty" json:"assetName,omitempty"`
	Alias        string `toml:"alias,omitempty" json:"alias,omitempty"`
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
			TerminalFontFamily: "monospace",
			TerminalFontSize:   12,
		},
		Behavior: Behavior{
			ConfirmCloseActiveSession: true,
			ShowTabCloseButtons:       true,
		},
		Workspace: Workspace{Tabs: []WorkspaceTab{}},
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
	if err := validateWorkspace(c.Workspace); err != nil {
		return err
	}
	return nil
}

func validateWorkspace(workspace Workspace) error {
	if len(workspace.Tabs) == 0 {
		if workspace.ActiveTabID != "" {
			return fmt.Errorf("workspace.active_tab_id must be empty when no tabs are saved")
		}
		return nil
	}
	ids := make(map[string]struct{}, len(workspace.Tabs))
	singletons := make(map[string]struct{}, 3)
	activeFound := false
	for _, tab := range workspace.Tabs {
		if strings.TrimSpace(tab.ID) == "" || tab.ID != strings.TrimSpace(tab.ID) {
			return fmt.Errorf("workspace tab ID is invalid")
		}
		if _, exists := ids[tab.ID]; exists {
			return fmt.Errorf("workspace tab ID %q is duplicated", tab.ID)
		}
		ids[tab.ID] = struct{}{}
		if tab.ID == workspace.ActiveTabID {
			activeFound = true
		}
		switch tab.Type {
		case "assets", "profiles", "settings":
			if _, exists := singletons[tab.Type]; exists {
				return fmt.Errorf("workspace tab type %q is duplicated", tab.Type)
			}
			singletons[tab.Type] = struct{}{}
		case "ssh":
			if tab.Profile == "" || tab.Organization == "" || tab.Target == "" || tab.Account == "" || tab.AssetID == "" || tab.AssetName == "" {
				return fmt.Errorf("workspace SSH tab %q has an incomplete connection descriptor", tab.ID)
			}
		default:
			return fmt.Errorf("workspace tab type %q is invalid", tab.Type)
		}
	}
	if !activeFound {
		return fmt.Errorf("workspace active tab %q does not exist", workspace.ActiveTabID)
	}
	return nil
}
