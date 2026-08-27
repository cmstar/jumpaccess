package cli

import (
	"fmt"

	authapp "github.com/cmstar/jumpaccess/internal/application/auth"
	"github.com/spf13/cobra"
)

func newAuthCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Authenticate with JumpServer using OAuth"}
	command.AddCommand(newAuthLoginCommand(deps))
	command.AddCommand(newAuthStatusCommand(deps))
	command.AddCommand(newAuthRefreshCommand(deps))
	command.AddCommand(newAuthLogoutCommand(deps))
	return command
}

func newAuthLoginCommand(deps Dependencies) *cobra.Command {
	var profile string
	command := &cobra.Command{
		Use:   "login",
		Short: "Open a browser and authenticate the selected profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := requireAuth(deps)
			if err != nil {
				return err
			}
			status, err := service.Login(cmd.Context(), profile)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "authenticated profile %s\n", status.Profile)
			return err
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	return command
}

func newAuthStatusCommand(deps Dependencies) *cobra.Command {
	var profile string
	command := &cobra.Command{
		Use:   "status",
		Short: "Show OAuth status without revealing credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := requireAuth(deps)
			if err != nil {
				return err
			}
			status, err := service.Status(profile)
			if err != nil {
				return err
			}
			return printAuthStatus(cmd, status)
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	return command
}

func newAuthRefreshCommand(deps Dependencies) *cobra.Command {
	var profile string
	command := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh OAuth credentials now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := requireAuth(deps)
			if err != nil {
				return err
			}
			status, err := service.Refresh(cmd.Context(), profile)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "refreshed profile %s\n", status.Profile)
			return err
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	return command
}

func newAuthLogoutCommand(deps Dependencies) *cobra.Command {
	var profile string
	command := &cobra.Command{
		Use:   "logout",
		Short: "Revoke and remove OAuth credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := requireAuth(deps)
			if err != nil {
				return err
			}
			if err := service.Logout(cmd.Context(), profile); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "logged out")
			return err
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "profile name (defaults to current profile)")
	return command
}

func requireAuth(deps Dependencies) (AuthService, error) {
	if deps.Auth == nil {
		return nil, fmt.Errorf("authentication service is unavailable")
	}
	return deps.Auth, nil
}

func printAuthStatus(cmd *cobra.Command, status authapp.Status) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "profile: %s\n", status.Profile); err != nil {
		return err
	}
	if !status.LoggedIn {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "status: not authenticated")
		return err
	}
	state := "authenticated"
	if status.Expired {
		state = "access token expired"
	}
	refresh := "no"
	if status.RefreshAvailable {
		refresh = "yes"
	}
	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"status: %s\naccess token expires: %s\nrefresh available: %s\n",
		state,
		status.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		refresh,
	)
	return err
}
