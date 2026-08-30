package guiconfig

import "testing"

func TestDefaultUsesTwelvePointTerminalFont(t *testing.T) {
	if got := Default().Appearance.TerminalFontSize; got != 12 {
		t.Fatalf("Default terminal font size = %d, want 12", got)
	}
}

func TestDefaultUsesUnsavedStandardWindowPlacement(t *testing.T) {
	got := Default().Window
	if got.HasBounds || got.Maximized || got.Width != 1280 || got.Height != 800 {
		t.Fatalf("Default window placement = %#v", got)
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
