package outbox

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/common"
	"github.com/container-registry/harbor-satellite/pkg/groundcontrol"
	"github.com/spf13/cobra"
)

func NewListCommand(runtime *common.Runtime) *cobra.Command {
	var params groundcontrol.ListOutboxEventsParams
	cmd := &cobra.Command{
		Use:   "outbox-events",
		Short: "List outbox events that are not done yet",
		Long: `List outbox events that are not done yet, oldest first. --state pending
(the default) lists every event that is not done, --state failed only those
that reached OUTBOX_MAX_ATTEMPTS. System administrators only.`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := runtime.ValidateAuth(); err != nil {
				return err
			}
			switch params.State {
			case "", "pending", "failed":
			default:
				return fmt.Errorf("--state must be pending or failed")
			}
			// The server falls back to 100 for a limit out of range instead of refusing it.
			if cmd.Flags().Changed("limit") && (params.Limit < 1 || params.Limit > 1000) {
				return fmt.Errorf("--limit must be between 1 and 1000")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			response, err := runtime.Client().ListOutboxEventsWithResponse(cmd.Context(), &params)
			if err != nil {
				return err
			}
			return common.PrintResponse(cmd, response)
		},
	}
	cmd.Flags().StringVar(&params.State, "state", "", "pending or failed (server default pending)")
	cmd.Flags().Int32Var(&params.Limit, "limit", 0, "maximum number of events, 1 to 1000")
	return cmd
}
