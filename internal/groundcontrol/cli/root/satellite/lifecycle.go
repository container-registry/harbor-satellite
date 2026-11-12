package satellite

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/spf13/cobra"
)

type satelliteCall func(ctx context.Context, runtime *common.Runtime, name string) (common.HTTPResponse, error)

func satelliteCommand(runtime *common.Runtime, flag, use, short, long string, call satelliteCall) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			return common.ValidateRequired(flag, name)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := call(cmd.Context(), runtime, name)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&name, flag, "", "satellite name")
	common.MarkRequired(cmd, flag)
	return cmd
}

func NewLifecycleCommand(runtime *common.Runtime) *cobra.Command {
	return satelliteCommand(runtime, "satellite", "satellite-lifecycle", "Get a satellite's lifecycle state and convergence",
		"Get the lifecycle state (ADR-0006) of a satellite and, per state artifact, the digest Ground Control published and the one the satellite applied.",
		func(ctx context.Context, runtime *common.Runtime, name string) (common.HTTPResponse, error) {
			return runtime.Client().GetSatelliteLifecycleWithResponse(ctx, name)
		})
}

func NewRevokeCommand(runtime *common.Runtime) *cobra.Command {
	return satelliteCommand(runtime, "name", "satellite", "Revoke a satellite",
		"Revoke a satellite: its heartbeats and ZTR get 403 and its Harbor robot is disabled. unrevoke satellite undoes it.",
		func(ctx context.Context, runtime *common.Runtime, name string) (common.HTTPResponse, error) {
			return runtime.Client().RevokeSatelliteWithResponse(ctx, name)
		})
}

func NewUnrevokeCommand(runtime *common.Runtime) *cobra.Command {
	return satelliteCommand(runtime, "name", "satellite", "Lift a satellite's revocation",
		"Lift a satellite's revocation. It stays pending until its next heartbeat.",
		func(ctx context.Context, runtime *common.Runtime, name string) (common.HTTPResponse, error) {
			return runtime.Client().UnrevokeSatelliteWithResponse(ctx, name)
		})
}
