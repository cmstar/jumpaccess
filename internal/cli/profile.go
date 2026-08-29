package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cmstar/jumpaccess/internal/application/settings"
	"github.com/spf13/cobra"
)

func newProfileCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Manage JumpServer profiles",
	}

	var siteURL string
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a JumpServer profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := settings.Service{Store: deps.Store, Credentials: deps.Credentials}
			if err := service.AddProfile(args[0], siteURL); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "profile %s added\n", args[0])
			return err
		},
	}
	add.Flags().StringVar(&siteURL, "url", "", "JumpServer site URL")
	_ = add.MarkFlagRequired("url")
	command.AddCommand(add)

	var updatedURL string
	update := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a JumpServer profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := settings.Service{Store: deps.Store, Credentials: deps.Credentials}
			if err := service.UpdateProfileURL(args[0], updatedURL); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "profile %s updated\n", args[0])
			return err
		},
	}
	update.Flags().StringVar(&updatedURL, "url", "", "JumpServer site URL")
	_ = update.MarkFlagRequired("url")
	command.AddCommand(update)

	command.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a JumpServer profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			value, err := deps.Store.Load()
			if err != nil {
				return err
			}
			if _, exists := value.Profiles[name]; !exists {
				return fmt.Errorf("profile %q does not exist", name)
			}
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Deleting profile %q removes its URL, Organization, Aliases, and OAuth credential. Type yes to confirm: ", name); err != nil {
				return err
			}
			answer := ""
			if deps.Stdin != nil {
				answer, err = bufio.NewReader(deps.Stdin).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("read deletion confirmation: %w", err)
				}
			}
			if strings.TrimRight(answer, "\r\n") != "yes" {
				_, err := fmt.Fprintln(cmd.ErrOrStderr(), "profile deletion cancelled")
				return err
			}
			service := settings.Service{Store: deps.Store, Credentials: deps.Credentials}
			if err := service.DeleteProfile(name); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "profile %s deleted\n", name)
			return err
		},
	})

	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured JumpServer profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, err := deps.Store.Load()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(value.Profiles))
			for name := range value.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)
			rows := make([][]string, 0, len(names))
			for _, name := range names {
				marker := ""
				if name == value.CurrentProfile {
					marker = "*"
				}
				rows = append(rows, []string{marker, name, value.Profiles[name].URL})
			}
			return writeTable(cmd.OutOrStdout(), []string{"CURRENT", "PROFILE", "URL"}, rows)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "use <name>",
		Short: "Select the current JumpServer profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return (settings.Service{Store: deps.Store}).UseProfile(args[0])
		},
	})
	return command
}
