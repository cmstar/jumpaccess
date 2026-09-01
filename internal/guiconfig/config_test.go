package guiconfig

import "testing"

func TestDefaultUsesTwelvePointTerminalFont(t *testing.T) {
	if got := Default().Appearance.TerminalFontSize; got != 12 {
		t.Fatalf("Default terminal font size = %d, want 12", got)
	}
}

func TestDefaultUsesSystemMonospaceFont(t *testing.T) {
	if got := Default().Appearance.TerminalFontFamily; got != "monospace" {
		t.Fatalf("Default terminal font family = %q, want monospace", got)
	}
}

func TestDefaultUsesUnsavedStandardWindowPlacement(t *testing.T) {
	got := Default().Window
	if got.HasBounds || got.Maximized || got.Width != 1280 || got.Height != 800 {
		t.Fatalf("Default window placement = %#v", got)
	}
}

func TestDefaultInitializesEmptyWorkspaceTabs(t *testing.T) {
	if got := Default().Workspace.Tabs; got == nil || len(got) != 0 {
		t.Fatalf("Default workspace tabs = %#v, want non-nil empty slice", got)
	}
}

func TestDecodeOldGUIConfigUsesDefaultWindowPlacement(t *testing.T) {
	got, err := Decode([]byte("" +
		"version = 1\n" +
		"[appearance]\n" +
		"theme = \"dark\"\n" +
		"terminal_font_family = \"Cascadia Mono\"\n" +
		"terminal_font_size = 14\n" +
		"[behavior]\n" +
		"confirm_close_active_session = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Window != Default().Window {
		t.Fatalf("Window placement = %#v, want %#v", got.Window, Default().Window)
	}
}

func TestDecodeAcceptsPersistedWorkspaceTabs(t *testing.T) {
	_, err := Decode([]byte("" +
		"version = 1\n" +
		"[workspace]\n" +
		"active_tab_id = \"ssh-1\"\n" +
		"[[workspace.tabs]]\n" +
		"id = \"assets\"\n" +
		"type = \"assets\"\n" +
		"[[workspace.tabs]]\n" +
		"id = \"ssh-1\"\n" +
		"type = \"ssh\"\n" +
		"profile = \"production\"\n" +
		"organization = \"org-1\"\n" +
		"target = \"production-web\"\n" +
		"account = \"account-1\"\n" +
		"asset_id = \"asset-1\"\n" +
		"asset_name = \"prod-web-01\"\n" +
		"alias = \"production-web\"\n"))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	_, err := Decode([]byte("" +
		"version = 1\n" +
		"unknown = true\n"))
	if err == nil {
		t.Fatal("Decode error = nil, want unknown field error")
	}
}

func TestConfigRejectsInvalidAppearance(t *testing.T) {
	tests := []struct {
		name  string
		value Config
	}{
		{name: "theme", value: Config{Version: CurrentVersion, Appearance: Appearance{Theme: "sepia", TerminalFontFamily: "JetBrains Mono", TerminalFontSize: 13}}},
		{name: "font size", value: Config{Version: CurrentVersion, Appearance: Appearance{Theme: "system", TerminalFontFamily: "JetBrains Mono", TerminalFontSize: 7}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.value.Validate(); err == nil {
				t.Fatal("Validate error = nil, want invalid appearance error")
			}
		})
	}
}

func TestConfigRejectsWindowBelowMinimumSize(t *testing.T) {
	value := Default()
	value.Window.HasBounds = true
	value.Window.Width = 800

	if err := value.Validate(); err == nil {
		t.Fatal("Validate error = nil, want invalid window size error")
	}
}

func TestConfigRejectsInvalidWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		workspace Workspace
	}{
		{
			name:      "active tab is missing",
			workspace: Workspace{ActiveTabID: "settings", Tabs: []WorkspaceTab{{ID: "assets", Type: "assets"}}},
		},
		{
			name: "duplicate tab id",
			workspace: Workspace{ActiveTabID: "assets", Tabs: []WorkspaceTab{
				{ID: "assets", Type: "assets"},
				{ID: "assets", Type: "profiles"},
			}},
		},
		{
			name: "duplicate singleton",
			workspace: Workspace{ActiveTabID: "assets-1", Tabs: []WorkspaceTab{
				{ID: "assets-1", Type: "assets"},
				{ID: "assets-2", Type: "assets"},
			}},
		},
		{
			name:      "unknown tab type",
			workspace: Workspace{ActiveTabID: "unknown", Tabs: []WorkspaceTab{{ID: "unknown", Type: "unknown"}}},
		},
		{
			name: "incomplete SSH descriptor",
			workspace: Workspace{ActiveTabID: "ssh-1", Tabs: []WorkspaceTab{{
				ID: "ssh-1", Type: "ssh", Profile: "production", Target: "asset-1",
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := Default()
			value.Workspace = test.workspace
			if err := value.Validate(); err == nil {
				t.Fatal("Validate error = nil, want invalid workspace error")
			}
		})
	}
}
