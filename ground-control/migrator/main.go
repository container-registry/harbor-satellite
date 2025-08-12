package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/url"
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

func parseDBConfig() DBConfig {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	u, err := url.Parse(dbURL)
	if err != nil {
		log.Fatalf("invalid DATABASE_URL: %v", err)
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		log.Fatalf("missing or invalid port in DATABASE_URL: %v", err)
	}

	return DBConfig{
		URL:  dbURL,
		Host: host,
		Port: port,
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
	provider, err := goose.NewProvider(goose.DialectPostgres, db, os.DirFS("."))
	if err != nil {
		log.Fatalf("failed to create goose provider: %v", err)
	}

	ctx := context.Background()
	if _, err := provider.Up(ctx); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	log.Println("Migrations completed successfully.")
}

func main() {
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
