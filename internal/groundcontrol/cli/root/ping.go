package root

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func PingCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Ping the Ground Control service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().PingWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}
