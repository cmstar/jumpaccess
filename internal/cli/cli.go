package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/spf13/cobra"
)

type Dependencies struct {
	Version    string
	ConfigPath string
	Store      projectconfig.Store
	OpenFile   func(string) error
	Stdout     io.Writer
	Stderr     io.Writer
}

func NewRoot(deps Dependencies) *cobra.Command {
	if deps.Stdout == nil {
		deps.Stdout = io.Discard
	}
	if deps.Stderr == nil {
		deps.Stderr = io.Discard
	}

	root := &cobra.Command{
		Use:           "jumpctl",
		Short:         "Access JumpServer assets from the command line",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the jumpctl version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "jumpctl %s\n", deps.Version)
			return err
		},
	})
	configCommand := &cobra.Command{
		Use:   "config",
		Short: "Inspect and edit the JumpAccess configuration",
	}
	configCommand.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config.toml path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), deps.ConfigPath)
			return err
		},
	})
	configCommand.AddCommand(&cobra.Command{
		Use:   "edit",
		Short: "Open config.toml in the system editor",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := os.Stat(deps.ConfigPath); errors.Is(err, fs.ErrNotExist) {
				if err := deps.Store.Save(projectconfig.Default()); err != nil {
					return err
				}
			} else if err != nil {
				return fmt.Errorf("inspect config: %w", err)
			}
			if deps.OpenFile == nil {
				return fmt.Errorf("config editor is unavailable")
			}
			return deps.OpenFile(deps.ConfigPath)
		},
	})
	configCommand.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := deps.Store.Load(); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "config valid")
			return err
		},
	})
	root.AddCommand(configCommand)
	root.AddCommand(newProfileCommand(deps))
	root.AddCommand(newAliasCommand(deps))
	return root
}
