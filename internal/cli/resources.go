package cli

import (
	"fmt"
	"sort"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/spf13/cobra"
)

func newResourceCommands(deps Dependencies) []*cobra.Command {
	return []*cobra.Command{
		newOrganizationCommand(deps),
		newAssetCommand(deps),
		newAccountCommand(deps),
	}
}

func newOrganizationCommand(deps Dependencies) *cobra.Command {
	var profile string
	command := &cobra.Command{Use: "organization", Aliases: []string{"org"}, Short: "Inspect permitted JumpServer organizations"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List permitted organizations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := requireResources(deps)
			if err != nil {
				return err
			}
			organizations, err := service.ListOrganizations(cmd.Context(), profile)
			if err != nil {
				return err
			}
			sort.Slice(organizations, func(i, j int) bool { return organizations[i].ID < organizations[j].ID })
			for _, organization := range organizations {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", organization.ID, organization.Name); err != nil {
					return err
				}
			}
			return nil
		},
	}
	list.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	command.AddCommand(list)
	return command
}

func newAssetCommand(deps Dependencies) *cobra.Command {
	var profile string
	var organization string
	var search string
	command := &cobra.Command{Use: "asset", Short: "Inspect permitted JumpServer assets"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List permitted assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := requireResources(deps)
			if err != nil {
				return err
			}
			page, err := service.ListAssets(cmd.Context(), profile, organization, search)
			if err != nil {
				return err
			}
			sort.Slice(page.Results, func(i, j int) bool { return page.Results[i].Name < page.Results[j].Name })
			for _, asset := range page.Results {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", asset.ID, asset.Name, asset.Address, asset.Type.Value); err != nil {
					return err
				}
			}
			return nil
		},
	}
	list.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	list.Flags().StringVar(&organization, "organization", "", "JumpServer organization ID")
	list.Flags().StringVar(&search, "search", "", "search by asset name, address, or ID")
	command.AddCommand(list)
	return command
}

func newAccountCommand(deps Dependencies) *cobra.Command {
	var profile string
	var organization string
	command := &cobra.Command{Use: "account", Short: "Inspect permitted JumpServer accounts"}
	list := &cobra.Command{
		Use:   "list <asset>",
		Short: "List permitted accounts for one exact asset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := requireResources(deps)
			if err != nil {
				return err
			}
			asset, err := service.FindAsset(cmd.Context(), profile, organization, args[0])
			if err != nil {
				return err
			}
			accounts := append([]jumpserver.Account(nil), asset.Accounts...)
			sort.Slice(accounts, func(i, j int) bool { return accountIdentity(accounts[i]) < accountIdentity(accounts[j]) })
			for _, account := range accounts {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", account.ID, account.Username, account.Name); err != nil {
					return err
				}
			}
			return nil
		},
	}
	list.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	list.Flags().StringVar(&organization, "organization", "", "JumpServer organization ID")
	command.AddCommand(list)
	return command
}

func requireResources(deps Dependencies) (ResourceService, error) {
	if deps.Resources == nil {
		return nil, fmt.Errorf("JumpServer resource service is unavailable")
	}
	return deps.Resources, nil
}

func accountIdentity(account jumpserver.Account) string {
	if account.Username != "" {
		return account.Username
	}
	if account.Name != "" {
		return account.Name
	}
	return account.ID
}
