package root

import (
	"errors"
	"fmt"
	"log"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/migrator"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/spf13/cobra"
)

// WorkerCommand runs the async plane without the HTTP server (plan item
// 2.4); several workers may run against one database.
func WorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run the Ground Control outbox publisher without the API server",
		Args:  cobra.NoArgs,
		// Hidden from the client-oriented help; it is a server process.
		Hidden: true,
		// Like serve, the worker needs no administration client.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			ctx := cmd.Context()
			if err = env.LoadGC(); err != nil {
				return fmt.Errorf("failed to load environment: %w", err)
			}
			if err = migrator.DoMigrations(ctx); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}
			cfg, err := dbpool.ConfigFromEnv(env.GC.Database)
			if err != nil {
				return fmt.Errorf("database settings: %w", err)
			}
			pools, err := dbpool.Open(ctx, cfg)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { err = errors.Join(err, pools.Close()) }()
			spire := workerSPIREClient(env.GC.SPIRE)
			if spire != nil {
				defer func() { err = errors.Join(err, spire.Close()) }()
			}
			w, closeWorker, err := newWorker(ctx, pools, spire)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Ground Control worker running")
			return errors.Join(w.Run(ctx), closeWorker())
		},
	}
}

// workerSPIREClient connects to SPIRE_SERVER_SOCKET so tombstones delete the
// SPIRE entries of deleted satellites. An embedded SPIRE server lives in the
// serve process, which a standalone worker cannot reach. A failed connection
// only warns, as it does in serve.
func workerSPIREClient(cfg env.SPIRE) *spiffe.ServerClient {
	if cfg.ServerSocket == "" {
		return nil
	}
	client, err := spiffe.NewServerClient(cfg.ServerSocket, cfg.TrustDomain)
	if err != nil {
		log.Printf("Warning: Failed to connect to external SPIRE server at %q: %v", cfg.ServerSocket, err)
		return nil
	}
	return client
}
