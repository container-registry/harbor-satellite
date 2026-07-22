package group

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewRemoveSatelliteCommand(runtime *common.Runtime) *cobra.Command {
	var satellite string
	var name string
	cmd := &cobra.Command{
		Use:   "satellite-from-group",
		Short: "Remove a satellite from a group",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("satellite", satellite, "group", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().RemoveSatelliteFromGroupWithResponse(cmd.Context(), groundcontrol.SatelliteGroupRequest{
				Satellite: satellite,
				Group:     name,
			})
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&satellite, "satellite", "", "satellite name")
	cmd.Flags().StringVar(&name, "group", "", "group name")
	common.MarkRequired(cmd, "satellite", "group")
	return cmd
}
