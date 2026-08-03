package root

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func HealthCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check the health of the Ground Control service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().HealthWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}
