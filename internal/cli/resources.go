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
			rows := make([][]string, 0, len(organizations))
			for _, organization := range organizations {
				rows = append(rows, []string{organization.ID, organization.Name})
			}
			return writeTable(cmd.OutOrStdout(), []string{"ID", "NAME"}, rows)
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
	var offset int
	var limit int
	command := &cobra.Command{Use: "asset", Short: "Inspect permitted JumpServer assets"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List permitted assets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if offset < 0 {
				return fmt.Errorf("--offset must be greater than or equal to 0")
			}
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}
			service, err := requireResources(deps)
			if err != nil {
				return err
			}
			page, err := service.ListAssets(cmd.Context(), profile, organization, search, offset, limit)
			if err != nil {
				return err
			}
			sort.Slice(page.Results, func(i, j int) bool { return page.Results[i].Name < page.Results[j].Name })
			rows := make([][]string, 0, len(page.Results))
			for _, asset := range page.Results {
				rows = append(rows, []string{asset.ID, asset.Name, asset.Address, asset.Type.Value})
			}
			return writeTable(cmd.OutOrStdout(), []string{"ID", "NAME", "ADDRESS", "TYPE"}, rows)
		},
	}
	list.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	list.Flags().StringVar(&organization, "organization", "", "JumpServer organization ID")
	list.Flags().StringVar(&search, "search", "", "search by asset name, address, or ID")
	list.Flags().IntVar(&offset, "offset", 0, "number of matching assets to skip")
	list.Flags().IntVar(&limit, "limit", 100, "maximum number of matching assets to return")
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
			rows := make([][]string, 0, len(accounts))
			for _, account := range accounts {
				rows = append(rows, []string{account.ID, account.Username, account.Name})
			}
			return writeTable(cmd.OutOrStdout(), []string{"ID", "USERNAME", "NAME"}, rows)
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
