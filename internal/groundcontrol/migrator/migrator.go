package migrator

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

func waitForPostgresReady(db *sql.DB, timeout time.Duration) error {
	const retryInterval = 2 * time.Second

	timeoutCtx, cancel := context.WithTimeout(context.Background(), timeout)
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
			log.Printf("timed out waiting for PostgreSQL readiness: %v", timeoutCtx.Err())
			return timeoutCtx.Err()
		}
	}
}

func runMigrations(db *sql.DB) error {
	migrationsPath := "/migrations"
	if _, err := os.Stat(migrationsPath); errors.Is(err, os.ErrNotExist) {
		migrationsPath = "internal/groundcontrol/sql/schema"
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS(migrationsPath))
	if err != nil {
		log.Fatalf("failed to create goose provider: %v", err)
	}

	if _, err := provider.Up(context.Background()); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	log.Println("Migrations completed successfully.")
	return nil
}

func DoMigrations() error {
	cfg := env.GC.Database

	db, err := sql.Open("postgres", cfg.URL())
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Fatalf("error closing DB: %v", err)
		}
	}()

	err = waitForPostgresReady(db, 60*time.Second)
	if err != nil {
		log.Fatalf("PostgreSQL is not ready: %v", err)
		return err
	}

	err = runMigrations(db)
	if err != nil {
		log.Fatalf("failed to run migrations: %v", err)
		return err
	}

	return nil
}
