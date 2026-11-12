package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harborhealth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/migrator"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/spf13/cobra"
)

// Values of serve --worker: where the outbox publisher runs (plan item
// 2.4). Embedded is the default; off leaves it to `groundcontrol worker`.
const (
	WorkerOff      = "off"
	WorkerEmbedded = "embedded"
)

type serveDependencies struct {
	loadEnvironment func() error
	// checkHealth only logs: Ground Control starts while Harbor is
	// unreachable and /health reports it degraded (plan item 2.6).
	checkHealth func(context.Context) error
	migrate     func(context.Context) error
	newServer   func(context.Context) (*server.ServerResult, error)
	// jobs are run beside the HTTP server until it stops: the registry
	// probe always, the async plane worker with --worker=embedded.
	jobs func(ctx context.Context, embedded bool) ([]job, error)
}

// job is a background part of the serve process.
type job struct {
	name string
	run  func(ctx context.Context) error
}

func ServeCommand() *cobra.Command {
	var composed composition
	return newServeCommand(serveDependencies{
		loadEnvironment: env.LoadGC,
		checkHealth:     harborhealth.CheckHealth,
		migrate:         migrator.DoMigrations,
		newServer: func(ctx context.Context) (*server.ServerResult, error) {
			return server.NewServer(ctx, server.WithFleetFactory(composed.fleetFactory))
		},
		jobs: composed.jobs,
	})
}

func newServeCommand(deps serveDependencies) *cobra.Command {
	var worker string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Ground Control server",
		Args:  cobra.NoArgs,
		// Override the root hook: a local server needs no administration client.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch worker {
			case WorkerOff, WorkerEmbedded:
			default:
				return fmt.Errorf("invalid --worker %q: want %s or %s", worker, WorkerOff, WorkerEmbedded)
			}
			return runServe(cmd.Context(), cmd.OutOrStdout(), deps, worker == WorkerEmbedded)
		},
	}
	cmd.Flags().StringVar(&worker, "worker", WorkerEmbedded, "where the outbox publisher runs: "+WorkerOff+" or "+WorkerEmbedded)
	return cmd
}

func runServe(ctx context.Context, out io.Writer, deps serveDependencies, embedded bool) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = deps.loadEnvironment(); err != nil {
		return fmt.Errorf("failed to load environment: %w", err)
	}
	if deps.checkHealth != nil {
		if healthErr := deps.checkHealth(ctx); healthErr != nil {
			_, _ = fmt.Fprintf(out, "Harbor is not reachable yet (%v); starting anyway, /health reports it degraded\n", healthErr)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	if err = deps.migrate(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	result, err := deps.newServer(ctx)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	defer func() { err = errors.Join(err, result.Close()) }()

	var jobs []job
	if deps.jobs != nil {
		if jobs, err = deps.jobs(ctx, embedded); err != nil {
			return fmt.Errorf("start background jobs: %w", err)
		}
	}
	jobCtx, stopJobs := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Go(func() {
			err := j.run(jobCtx)
			if jobCtx.Err() != nil {
				return
			}
			// Jobs return only when they cannot go on (ADR-0014), so the
			// replica keeps serving but must leave the load balancer.
			_, _ = fmt.Fprintf(out, "%s stopped: %v; /health reports unhealthy\n", j.name, err)
			result.AppServer.JobStopped(j.name)
		})
	}
	defer func() {
		stopJobs()
		wg.Wait()
	}()

	return serveHTTP(ctx, out, result, 5*time.Second)
}

func serveHTTP(ctx context.Context, out io.Writer, result *server.ServerResult, shutdownTimeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cleanupCtx, cleanupCancel := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		result.AppServer.StartCleanupJob(cleanupCtx, server.NewCleanupConfig())
	}()
	defer func() {
		cleanupCancel()
		<-cleanupDone
	}()

	listenerDone := make(chan error, 1)
	go func() { listenerDone <- listenAndServe(out, result) }()

	select {
	case err := <-listenerDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("cannot start server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	cleanupCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := result.Server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		// Shutdown leaves active connections open on timeout; force them closed.
		shutdownErr = errors.Join(shutdownErr, result.Server.Close())
	}
	listenerErr := <-listenerDone
	if errors.Is(listenerErr, http.ErrServerClosed) {
		listenerErr = nil
	}
	if err := errors.Join(shutdownErr, listenerErr); err != nil {
		return fmt.Errorf("HTTP shutdown: %w", err)
	}
	return nil
}

func listenAndServe(out io.Writer, result *server.ServerResult) error {
	switch {
	case result.SPIFFEConfig != nil && result.SPIFFEConfig.Enabled:
		_, _ = fmt.Fprintf(out, "Starting Ground Control with SPIFFE mTLS on %s\n", result.Server.Addr)
		return result.Server.ListenAndServeTLS("", "")
	case result.TLSConfig != nil && result.TLSConfig.Enabled:
		_, _ = fmt.Fprintf(out, "Starting Ground Control with TLS on %s\n", result.Server.Addr)
		return result.Server.ListenAndServeTLS(result.TLSConfig.CertFile, result.TLSConfig.KeyFile)
	default:
		_, _ = fmt.Fprintf(out, "Starting Ground Control on %s\n", result.Server.Addr)
		return result.Server.ListenAndServe()
	}
}
