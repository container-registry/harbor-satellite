package spire

import (
	"strings"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewAgentsCommand(runtime *common.Runtime) *cobra.Command {
	var attestationType string
	cmd := &cobra.Command{
		Use:     "spire-agents",
		Short:   "List attested SPIRE agents",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := &groundcontrol.ListSpireAgentsParams{}
			if strings.TrimSpace(attestationType) != "" {
				params.AttestationType = &attestationType
			}
			response, err := runtime.Client().ListSpireAgentsWithResponse(cmd.Context(), params)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&attestationType, "attestation-type", "", "filter by attestation type")
	return cmd
}

func NewStatusCommand(runtime *common.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:     "spire-status",
		Short:   "Get SPIRE integration status",
		Args:    cobra.NoArgs,
		PreRunE: common.RequiredAuth(runtime),
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().GetSpireStatusWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
}
