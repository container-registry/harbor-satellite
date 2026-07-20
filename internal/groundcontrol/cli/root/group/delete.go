package group

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewDeleteCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Delete a group",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("name", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().DeleteGroupWithResponse(cmd.Context(), name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "group name")
	common.MarkRequired(cmd, "name")
	return cmd
}
