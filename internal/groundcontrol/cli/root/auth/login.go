package auth

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewLoginCommand(runtime *common.Runtime) *cobra.Command {
	var username string
	var password string
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Create a Ground Control session",
		Long:  "Create a Ground Control session using the password in GROUND_CONTROL_PASSWORD.",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := common.ValidateRequired("username", username); err != nil {
				return err
			}
			var err error
			password, err = common.RequiredEnv("GROUND_CONTROL_PASSWORD")
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().LoginWithResponse(cmd.Context(), groundcontrol.LoginRequest{
				Username: username,
				Password: password,
			})
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	loginCmd.Flags().StringVar(&username, "username", "", "Ground Control username")
	common.MarkRequired(loginCmd, "username")
	return loginCmd
}
