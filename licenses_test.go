package jumpaccess

import (
	"strings"
	"testing"
)

func TestLicensesContainProjectAndDependencyTerms(t *testing.T) {
	text := Licenses()
	for _, want := range []string{
		"MIT License",
		"Copyright (c) 2026 Eric Ruan",
		"THIRD-PARTY SOFTWARE NOTICES AND LICENSES",
		"github.com/BurntSushi/toml v1.6.0",
		"github.com/spf13/cobra v1.10.2",
		"github.com/inconshreveable/mousetrap v1.1.0",
		"github.com/spf13/pflag v1.0.9",
		"github.com/wailsapp/wails/v2 v2.14.0",
		"github.com/wailsapp/go-webview2 v1.0.22",
		"github.com/pkg/errors v0.9.1",
		"golang.org/x/crypto v0.53.0",
		"golang.org/x/sys v0.46.0",
		"golang.org/x/term v0.44.0",
		"react v19.1.1",
		"lucide-react v1.37.0",
		"@xterm/xterm v6.0.0",
		"@xterm/addon-fit v0.11.0",
		"catppuccin/windows-terminal",
		"dracula/windows-terminal",
		"nordtheme/alacritty",
		"folke/tokyonight.nvim",
		"sonph/onehalf",
		"altercation/solarized",
		"ISC License",
		"Apache License",
		"Copyright 2009 The Go Authors",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Licenses() does not contain %q", want)
		}
	}
}
