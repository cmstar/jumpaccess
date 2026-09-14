package cli

import (
	"fmt"

	"github.com/cmstar/jumpaccess/internal/application/settings"
	projectconfig "github.com/cmstar/jumpaccess/internal/config"
	"github.com/spf13/cobra"
)

func newAliasCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "alias",
		Short: "Manage profile-scoped asset aliases",
	}

	var profileName string
	var asset string
	var account string
	var organization string
	command.PersistentFlags().StringVar(&profileName, "profile", "", "Profile name (defaults to current)")
	set := &cobra.Command{
		Use:   "set <name>",
		Short: "Create or replace an asset alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return (settings.Service{Store: deps.Store}).SetAlias(profileName, args[0], projectconfig.Alias{
				Asset:        asset,
				Account:      account,
				Organization: organization,
			})
		},
	}
	set.Flags().StringVar(&asset, "asset", "", "Asset ID, name, or address")
	set.Flags().StringVar(&account, "account", "", "Account ID, alias, name, or username")
	set.Flags().StringVar(&organization, "organization", "", "Organization ID")
	_ = set.MarkFlagRequired("asset")
	command.AddCommand(set)
	var long bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List asset aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, err := deps.Store.Load()
			if err != nil {
				return err
			}
			selectedProfile := profileName
			if selectedProfile == "" {
				selectedProfile = value.CurrentProfile
			}
			profile, ok := value.Profiles[selectedProfile]
			if !ok {
				return fmt.Errorf("profile %q does not exist", selectedProfile)
			}
			entries, err := listAliases(cmd.Context(), deps, selectedProfile, profile)
			if err != nil {
				return err
			}
			return writeAliases(cmd.OutOrStdout(), entries, long)
		},
	}
	list.Flags().BoolVar(&long, "long", false, "Show each alias with full original references")
	command.AddCommand(list)
	return command
}
