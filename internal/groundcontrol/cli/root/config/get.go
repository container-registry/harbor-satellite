package config

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "configs",
		Short:   "List satellite configurations",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListConfigsWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewGetCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get a satellite configuration",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("name", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetConfigWithResponse(cmd.Context(), name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "configuration name")
	common.MarkRequired(cmd, "name")
	return cmd
}
