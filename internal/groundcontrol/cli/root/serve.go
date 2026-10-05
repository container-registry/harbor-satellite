package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harborhealth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/migrator"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/spf13/cobra"
)

type serveDependencies struct {
	loadEnvironment func() error
	checkHealth     func(context.Context) error
	migrate         func(context.Context) error
	newServer       func(context.Context) (*server.ServerResult, error)
}

func ServeCommand() *cobra.Command {
	return newServeCommand(serveDependencies{
		loadEnvironment: env.LoadGC,
		checkHealth:     harborhealth.CheckHealth,
		migrate:         migrator.DoMigrations,
		newServer:       server.NewServer,
	})
}

func newServeCommand(deps serveDependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the Ground Control server",
		Args:  cobra.NoArgs,
		// Override the root hook: a local server needs no administration client.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
}

func runServe(ctx context.Context, out io.Writer, deps serveDependencies) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := deps.loadEnvironment(); err != nil {
		return fmt.Errorf("failed to load environment: %w", err)
	}
	if err := deps.checkHealth(ctx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	if err := deps.migrate(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	result, err := deps.newServer(ctx)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	defer func() { err = errors.Join(err, result.Close()) }()

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
