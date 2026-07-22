package user

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewCreateCommand(runtime *common.Runtime) *cobra.Command {
	var username string
	var password string
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Create an admin user",
		Long:  "Create an admin user using the password in GROUND_CONTROL_USER_PASSWORD.",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			if err := common.ValidateRequired("username", username); err != nil {
				return err
			}
			var err error
			password, err = common.RequiredEnv("GROUND_CONTROL_USER_PASSWORD")
			return err
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().CreateUserWithResponse(cmd.Context(), groundcontrol.CreateUserRequest{
				Username: username,
				Password: password,
			})
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
