package auth

import (
	"fmt"

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
		Long:  "Create a Ground Control session using GROUND_CONTROL_PASSWORD and save it in the OS user configuration directory.",
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
			if err := common.ResponseError(response); err != nil {
				return err
			}
			if response.JSON200 == nil {
				return fmt.Errorf("login response did not contain a session")
			}
			return runtime.StoreToken(username, response.JSON200.Token, response.JSON200.ExpiresAt)
		},
	}
	loginCmd.Flags().StringVar(&username, "username", "", "Ground Control username")
	common.MarkRequired(loginCmd, "username")
	return loginCmd
}
