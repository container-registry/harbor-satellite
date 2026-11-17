package migrator

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	migrations "github.com/container-registry/harbor-satellite/internal/groundcontrol/sql"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
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

// Run applies the migrations in /migrations when present, else the embedded
// schema.
func Run(ctx context.Context, db *sql.DB) error {
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

	// GC replicas and workers may start together against one database.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, schema, goose.WithSessionLocker(locker))
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

	// Migrations run on the API plane's DSN: DB_URL_API when set.
	pools, err := dbpool.ConfigFromEnv(cfg)
	if err != nil {
		return err
	}
	db, err := sql.Open(dbpool.DriverName, pools.API.DSN)
	if err != nil {
		log.Println("failed to open DB connection")
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("error closing DB: %v", closeErr)
		}
	}()

	err = waitForPostgresReady(ctx, db, 60*time.Second)
	if err != nil {
		log.Println("PostgreSQL is not ready for queries")
		return err
	}

	if err = CheckServer(ctx, db); err != nil {
		return err
	}

	err = Run(ctx, db)
	if err != nil {
		log.Println("failed to run migrations")
		return err
	}

	return nil
}
