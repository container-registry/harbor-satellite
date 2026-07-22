package satellite

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "satellites",
		Short:   "List satellites",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListSatellitesWithResponse(cmd.Context())
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
		Use:   "satellite",
		Short: "Get a satellite",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("name", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetSatelliteWithResponse(cmd.Context(), name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "satellite name")
	common.MarkRequired(cmd, "name")
	return cmd
}

func NewActiveCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "active-satellites",
		Short:   "List active satellites",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListActiveSatellitesWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewStaleCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "stale-satellites",
		Short:   "List stale satellites",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListStaleSatellitesWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}

func NewCachedImagesCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "cached-images",
		Short: "List images cached by a satellite",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("satellite", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetCachedImagesWithResponse(cmd.Context(), name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "satellite", "", "satellite name")
	common.MarkRequired(cmd, "satellite")
	return cmd
}

func NewStatusCommand(runtime *common.Runtime) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "satellite-status",
		Short: "Get the latest satellite status report",
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired("satellite", name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetSatelliteStatusWithResponse(cmd.Context(), name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, "satellite", "", "satellite name")
	common.MarkRequired(cmd, "satellite")
	return cmd
}
