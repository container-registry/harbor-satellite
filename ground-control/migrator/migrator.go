package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

type DBConfig struct {
	URL  string
	Host string
	Port string
}

var (
	dbName   = os.Getenv("DB_DATABASE")
	password = os.Getenv("DB_PASSWORD")
	username = os.Getenv("DB_USERNAME")
	PORT     = os.Getenv("DB_PORT")
	HOST     = os.Getenv("DB_HOST")
)

func parseDBConfig() DBConfig {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		username,
		password,
		HOST,
		PORT,
		dbName,
	)

	return DBConfig{
		URL:  dbURL,
		Host: HOST,
		Port: PORT,
	}
}

func waitForPostgresReady(db *sql.DB, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			log.Fatalf("timed out waiting for PostgreSQL readiness")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := db.PingContext(ctx)
		cancel()

		if err == nil {
			log.Println("PostgreSQL is ready for queries.")
			return
		}

		log.Println("Waiting for PostgreSQL...")
		time.Sleep(2 * time.Second)
	}
}

func runMigrations(db *sql.DB) {
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("sql/schema"))
	if err != nil {
		log.Fatalf("failed to create goose provider: %v", err)
	}

	ctx := context.Background()
	if _, err := provider.Up(ctx); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	log.Println("Migrations completed successfully.")
}

func DoMigrations() {
	cfg := parseDBConfig()

	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Fatalf("error closing DB: %v", err)
		}
	}()

	waitForPostgresReady(db, 60*time.Second)
	runMigrations(db)
}
