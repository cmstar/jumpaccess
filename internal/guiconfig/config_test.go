package guiconfig

import "testing"

func TestDefaultUsesTwelvePointTerminalFont(t *testing.T) {
	if got := Default().Appearance.TerminalFontSize; got != 12 {
		t.Fatalf("Default terminal font size = %d, want 12", got)
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
