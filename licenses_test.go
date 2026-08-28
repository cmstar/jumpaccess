package jumpaccess

import (
	"strings"
	"testing"
)

func TestLicensesContainProjectAndDependencyTerms(t *testing.T) {
	text := Licenses()
	for _, want := range []string{
		"MIT License",
		"Copyright (c) 2026 cmstar",
		"THIRD-PARTY SOFTWARE NOTICES AND LICENSES",
		"github.com/BurntSushi/toml v1.6.0",
		"github.com/spf13/cobra v1.10.2",
		"github.com/inconshreveable/mousetrap v1.1.0",
		"github.com/spf13/pflag v1.0.9",
		"golang.org/x/crypto v0.46.0",
		"golang.org/x/sys v0.41.0",
		"golang.org/x/term v0.38.0",
		"Apache License",
		"Copyright 2009 The Go Authors",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Licenses() does not contain %q", want)
		}
	}
}
