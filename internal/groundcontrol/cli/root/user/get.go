package user

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "users",
		Short:   "List non-system users",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListUsersWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewGetCommand(runtime *common.Runtime) *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Get a user",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("username", username)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetUserWithResponse(cmd.Context(), username)
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
