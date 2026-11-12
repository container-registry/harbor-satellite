package root

import (
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
	"github.com/spf13/cobra"
)

// WorkerCommand runs the async plane without the HTTP server (plan item
// 2.4). Hidden until it works.
func WorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "worker",
		Short:  "Run the Ground Control outbox publisher without the API server",
		Args:   cobra.NoArgs,
		Hidden: true,
		// Like serve, the worker needs no administration client.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(_ *cobra.Command, _ []string) error {
			return notimpl.Err("2.4 standalone worker")
		},
	}
}
