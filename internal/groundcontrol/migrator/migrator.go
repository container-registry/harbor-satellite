package migrator

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log"
	"os"
	"time"

	migrations "github.com/container-registry/harbor-satellite/internal/groundcontrol/sql"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

func waitForPostgresReady(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	const retryInterval = 2 * time.Second

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		pingCtx, pingCancel := context.WithTimeout(timeoutCtx, 2*time.Second)
		err := db.PingContext(pingCtx)
		pingCancel()

		if err == nil {
			log.Println("PostgreSQL is ready for queries.")
			return nil
		}

		log.Printf("PostgreSQL is not ready: %v; retrying in %s", err, retryInterval)

		select {
		case <-time.After(retryInterval):
		case <-timeoutCtx.Done():
			log.Println("timed out waiting for PostgreSQL readiness")
			return timeoutCtx.Err()
		}
	}
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	schema, err := fs.Sub(migrations.Schema, "schema")
	if err != nil {
		return err
	}
	_, err = os.Stat("/migrations")
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			// Use embedded migrations when no container override is mounted.
		default:
			log.Println("failed to access migrations path")
			return err
		}
	} else {
		schema = os.DirFS("/migrations")
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, schema)
	if err != nil {
		log.Println("failed to create goose provider")
		return err
	}

	if _, err := provider.Up(ctx); err != nil {
		log.Println("failed to run migrations")
		return err
	}

	log.Println("Migrations completed successfully.")
	return nil
}

func DoMigrations(ctx context.Context) error {
	cfg := env.GC.Database

	db, err := sql.Open("postgres", cfg.URL())
	if err != nil {
		log.Println("failed to open DB connection")
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("error closing DB: %v", err)
		}
	}()

	err = waitForPostgresReady(ctx, db, 60*time.Second)
	if err != nil {
		log.Println("PostgreSQL is not ready for queries")
		return err
	}

	err = runMigrations(ctx, db)
	if err != nil {
		log.Println("failed to run migrations")
		return err
	}

	return nil
}
