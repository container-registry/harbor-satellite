package auth

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewLogoutCommand(runtime *common.Runtime) *cobra.Command {
	logoutCmd := &cobra.Command{
		Use:     "logout",
		Short:   "Delete the current Ground Control session",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().LogoutWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	return logoutCmd
}
