package cli

import (
	"fmt"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/target"
	"github.com/spf13/cobra"
)

func newSSHCommand(deps Dependencies) *cobra.Command {
	var profile string
	var organization string
	var account string
	command := &cobra.Command{
		Use:   "ssh <target>",
		Short: "Open an interactive SSH session through JumpServer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Connect == nil || deps.RunSSH == nil {
				return fmt.Errorf("SSH service is unavailable")
			}
			prepared, err := deps.Connect.Prepare(cmd.Context(), connectapp.Options{
				Target: target.Input{
					Target:       args[0],
					Profile:      profile,
					Organization: organization,
					Account:      account,
				},
				SelectAccount: deps.SelectAccount,
			})
			if err != nil {
				return err
			}
			return deps.RunSSH(cmd.Context(), prepared)
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	command.Flags().StringVar(&organization, "organization", "", "JumpServer organization ID")
	command.Flags().StringVar(&account, "account", "", "account ID, alias, name, or username")
	return command
}
