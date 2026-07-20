package group

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "groups",
		Short:   "List groups",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListGroupsWithResponse(cmd.Context())
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
		Use:   "group",
		Short: "Get a group",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("name", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetGroupWithResponse(cmd.Context(), name)
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

func NewSatellitesCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "group-satellites",
		Short: "List satellites attached to a group",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("group", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListGroupSatellitesWithResponse(cmd.Context(), name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "group", "", "group name")
	common.MarkRequired(cmd, "group")
	return cmd
}
