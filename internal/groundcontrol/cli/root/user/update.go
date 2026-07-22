package user

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewUpdateOwnPasswordCommand(runtime *common.Runtime) *cobra.Command {
	var currentPassword string
	var newPassword string
	cmd := &cobra.Command{
		Use:   "own-password",
		Short: "Change the authenticated user's password",
		Long:  "Change the authenticated user's password using GROUND_CONTROL_CURRENT_PASSWORD and GROUND_CONTROL_NEW_PASSWORD.",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			var err error
			currentPassword, err = common.RequiredEnv("GROUND_CONTROL_CURRENT_PASSWORD")
			if err != nil {
				return err
			}
			newPassword, err = common.RequiredEnv("GROUND_CONTROL_NEW_PASSWORD")
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ChangeOwnPasswordWithResponse(cmd.Context(), groundcontrol.ChangePasswordRequest{
				CurrentPassword: currentPassword,
				NewPassword:     newPassword,
			})
			if err != nil {
				return err
			}
			if response.StatusCode() >= 200 && response.StatusCode() < 300 {
				if err := runtime.RemoveStoredToken(); err != nil {
					return err
				}
			}
			return common.PrintResponse(cmd, response)
		},
	}
	return cmd
}

func NewUpdatePasswordCommand(runtime *common.Runtime) *cobra.Command {
	var username string
	var newPassword string
	cmd := &cobra.Command{
		Use:   "user-password",
		Short: "Reset a user's password",
		Long:  "Reset a user's password using GROUND_CONTROL_NEW_PASSWORD.",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("username", username); err != nil {
				return err
			}
			var err error
			newPassword, err = common.RequiredEnv("GROUND_CONTROL_NEW_PASSWORD")
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ChangeUserPasswordWithResponse(
				cmd.Context(),
				username,
				groundcontrol.ChangeUserPasswordRequest{NewPassword: newPassword},
			)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "username")
	common.MarkRequired(cmd, "username")
	return cmd
}
