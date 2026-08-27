package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cmstar/jumpaccess/internal/appdir"
	"github.com/cmstar/jumpaccess/internal/cli"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/cmstar/jumpaccess/internal/systemopen"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	rootDir, err := appdir.Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	configPath := filepath.Join(rootDir, "config.toml")
	command := cli.NewRoot(cli.Dependencies{
		Version:    version,
		ConfigPath: configPath,
		Store:      projectconfig.Store{Path: configPath},
		OpenFile:   systemopen.Open,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
