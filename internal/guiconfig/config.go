package guiconfig

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 3

const (
	TerminalRightClickPaste       = "paste"
	TerminalRightClickContextMenu = "context_menu"
)

const (
	DefaultWindowWidth  = 1280
	DefaultWindowHeight = 800
	MinimumWindowWidth  = 960
	MinimumWindowHeight = 640
)

type Config struct {
	Version    int             `toml:"version"`
	Appearance Appearance      `toml:"appearance"`
	Terminal   Terminal        `toml:"terminal"`
	Tabs       Tabs            `toml:"tabs"`
	Workspace  Workspace       `toml:"workspace" json:"-"`
	Window     WindowPlacement `toml:"window" json:"-"`
}

type Appearance struct {
	Theme string `toml:"theme"`
}

type Terminal struct {
	FontFamily       string `toml:"font_family"`
	FontSize         int    `toml:"font_size"`
	RightClickAction string `toml:"right_click_action"`
}

type Tabs struct {
	ConfirmCloseActiveSession bool `toml:"confirm_close_active_session"`
	ShowCloseButtons          bool `toml:"show_close_buttons"`
}

type legacyConfig struct {
	Version    int              `toml:"version"`
	Appearance legacyAppearance `toml:"appearance"`
	Behavior   legacyBehavior   `toml:"behavior"`
	Workspace  Workspace        `toml:"workspace"`
	Window     WindowPlacement  `toml:"window"`
}

type legacyAppearance struct {
	Theme              string `toml:"theme"`
	TerminalFontFamily string `toml:"terminal_font_family"`
	TerminalFontSize   int    `toml:"terminal_font_size"`
}

type legacyBehavior struct {
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

// WindowPlacement 保存桌面窗口最近一次退出时的状态。Display 标识目标显示器，
// X/Y 是该显示器工作区内的相对坐标；坐标和尺寸始终表示普通窗口状态，
// 避免最大化或最小化时的临时边界覆盖用户调整过的位置和大小。
type WindowPlacement struct {
	HasBounds bool   `toml:"has_bounds"`
	Maximized bool   `toml:"maximized"`
	Display   string `toml:"display,omitempty"`
	X         int    `toml:"x"`
	Y         int    `toml:"y"`
	Width     int    `toml:"width"`
	Height    int    `toml:"height"`
}

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Appearance: Appearance{
			Theme: "system",
		},
		Terminal: Terminal{
			FontFamily:       "monospace",
			FontSize:         12,
			RightClickAction: TerminalRightClickPaste,
		},
		Tabs: Tabs{
			ConfirmCloseActiveSession: true,
			ShowCloseButtons:          true,
		},
		Workspace: Workspace{Tabs: []WorkspaceTab{}},
		Window: WindowPlacement{
			Width:  DefaultWindowWidth,
			Height: DefaultWindowHeight,
		},
	}
}

func Decode(data []byte) (Config, error) {
	header := struct {
		Version int `toml:"version"`
	}{Version: CurrentVersion}
	if _, err := toml.Decode(string(data), &header); err != nil {
		return Config{}, fmt.Errorf("decode GUI TOML: %w", err)
	}
	if header.Version == 1 || header.Version == 2 {
		return decodeLegacy(data, header.Version)
	}
	if header.Version > CurrentVersion {
		return Config{}, fmt.Errorf("GUI config version %d is newer than supported version %d; update JumpAccess", header.Version, CurrentVersion)
	}
	if header.Version != CurrentVersion {
		return Config{}, fmt.Errorf("unsupported GUI config version %d", header.Version)
	}

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

func decodeLegacy(data []byte, version int) (Config, error) {
	defaults := Default()
	legacy := legacyConfig{
		Version: version,
		Appearance: legacyAppearance{
			Theme:              defaults.Appearance.Theme,
			TerminalFontFamily: defaults.Terminal.FontFamily,
			TerminalFontSize:   defaults.Terminal.FontSize,
		},
		Behavior: legacyBehavior{
			ConfirmCloseActiveSession: defaults.Tabs.ConfirmCloseActiveSession,
			ShowTabCloseButtons:       defaults.Tabs.ShowCloseButtons,
		},
		Workspace: defaults.Workspace,
		Window:    defaults.Window,
	}
	metadata, err := toml.Decode(string(data), &legacy)
	if err != nil {
		return Config{}, fmt.Errorf("decode GUI TOML: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("unknown GUI TOML field %q", undecoded[0].String())
	}
	result := defaults
	result.Appearance.Theme = legacy.Appearance.Theme
	result.Terminal.FontFamily = legacy.Appearance.TerminalFontFamily
	result.Terminal.FontSize = legacy.Appearance.TerminalFontSize
	result.Tabs.ConfirmCloseActiveSession = legacy.Behavior.ConfirmCloseActiveSession
	result.Tabs.ShowCloseButtons = legacy.Behavior.ShowTabCloseButtons
	result.Workspace = legacy.Workspace
	result.Window = legacy.Window
	// Version 1 没有保存显示器标识；X/Y 在 Windows 是虚拟桌面绝对坐标，
	// 在 macOS 是当前显示器相对坐标。保留原值并由窗口恢复逻辑完成一次性解释。
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
	font := strings.TrimSpace(c.Terminal.FontFamily)
	if font == "" || font != c.Terminal.FontFamily {
		return fmt.Errorf("terminal.font_family is invalid")
	}
	for _, character := range font {
		if unicode.IsControl(character) {
			return fmt.Errorf("terminal.font_family is invalid")
		}
	}
	if c.Terminal.FontSize < 9 || c.Terminal.FontSize > 32 {
		return fmt.Errorf("terminal.font_size must be between 9 and 32")
	}
	switch c.Terminal.RightClickAction {
	case TerminalRightClickPaste, TerminalRightClickContextMenu:
	default:
		return fmt.Errorf("terminal.right_click_action must be paste or context_menu")
	}
	if c.Window.Width < MinimumWindowWidth || c.Window.Height < MinimumWindowHeight {
		return fmt.Errorf("window size must be at least %dx%d", MinimumWindowWidth, MinimumWindowHeight)
	}
	if display := strings.TrimSpace(c.Window.Display); display != c.Window.Display {
		return fmt.Errorf("window.display is invalid")
	}
	for _, character := range c.Window.Display {
		if unicode.IsControl(character) {
			return fmt.Errorf("window.display is invalid")
		}
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
