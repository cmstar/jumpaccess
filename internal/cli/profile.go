package cli

import (
	"fmt"
	"sort"

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
			service := settings.Service{Store: deps.Store}
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
			for _, name := range names {
				marker := " "
				if name == value.CurrentProfile {
					marker = "*"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\n", marker, name, value.Profiles[name].URL); err != nil {
					return err
				}
			}
			return nil
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
