package guiconfig

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestTerminalStyleDefaultsAndLegacyCompatibility(t *testing.T) {
	for version := 1; version <= CurrentVersion; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			value, err := Decode([]byte(fmt.Sprintf("version = %d\n", version)))
			if err != nil {
				t.Fatal(err)
			}
			if value.Version != CurrentVersion || value.Terminal.LineHeight != 1 || value.Terminal.CursorStyle != "block" || !value.Terminal.CursorBlink {
				t.Fatalf("unexpected terminal defaults: %#v", value.Terminal)
			}
		})
	}
	value, err := Decode([]byte("version = 5\n[terminal]\ncolor_scheme = \"catppuccin-latte\"\nfont_size = 18\nright_click_action = \"context_menu\"\nwarn_on_multi_line_paste = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if value.Terminal.ColorScheme != "catppuccin-latte" || value.Terminal.FontSize != 18 || value.Terminal.RightClickAction != "context_menu" || value.Terminal.WarnOnMultiLinePaste {
		t.Fatalf("lost existing preferences: %#v", value.Terminal)
	}
}

func TestTerminalLineHeightValidation(t *testing.T) {
	for _, height := range []float64{1, 1.25, 1.5, 2} {
		value := Default()
		value.Terminal.LineHeight = height
		if err := value.Validate(); err != nil {
			t.Fatalf("height %v: %v", height, err)
		}
	}
	for _, height := range []float64{0, 0.9, 2.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		value := Default()
		value.Terminal.LineHeight = height
		if err := value.Validate(); err == nil || !strings.Contains(err.Error(), "terminal.line_height") {
			t.Fatalf("height %v: expected line height error, got %v", height, err)
		}
	}
}

func TestTerminalCursorStyleValidation(t *testing.T) {
	for _, style := range []string{"block", "bar", "underline", "quarter_block"} {
		value := Default()
		value.Terminal.CursorStyle = style
		if err := value.Validate(); err != nil {
			t.Fatalf("style %s: %v", style, err)
		}
	}
	for _, style := range []string{"", "outline", "beam", "BLOCK"} {
		value := Default()
		value.Terminal.CursorStyle = style
		if err := value.Validate(); err == nil || !strings.Contains(err.Error(), "terminal.cursor_style") {
			t.Fatalf("style %q: expected cursor style error, got %v", style, err)
		}
	}
}
