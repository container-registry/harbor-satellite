package satellite

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewUpdateConfigCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	var configName string
	cmd := &cobra.Command{
		Use:   "satellite-config",
		Short: "Assign a configuration to a satellite",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("satellite", name, "config", configName)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().SetSatelliteConfigWithResponse(cmd.Context(), groundcontrol.SatelliteConfigRequest{
				Satellite:  name,
				ConfigName: configName,
			})
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "satellite", "", "satellite name")
	cmd.Flags().StringVar(&configName, "config", "", "configuration name")
	common.MarkRequired(cmd, "satellite", "config")
	return cmd
}
