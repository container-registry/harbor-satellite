// Package dbtest gives Ground Control tests a real, migrated Postgres
// database. Point TEST_DATABASE_URL at a server where the user may create
// databases (`task test:integration` starts one); tests skip when it is unset.
package dbtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	migrations "github.com/container-registry/harbor-satellite/internal/groundcontrol/sql"
	"github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

// EnvURL names the variable holding the admin connection URL.
const EnvURL = "TEST_DATABASE_URL"

// DB is a database created for one test and dropped when it ends.
type DB struct {
	*sql.DB

	// URL connects to this database.
	URL      string
	Name     string
	Host     string
	Port     string
	User     string
	Password string
}

// Enabled reports whether integration tests can run.
func Enabled() bool {
	return os.Getenv(EnvURL) != ""
}

// New creates a fresh database, applies every embedded goose migration and
// enables pg_stat_statements in it. A database per test, rather than a
// schema, keeps statement statistics apart: they are recorded per database.
func New(t testing.TB) *DB {
	t.Helper()
	adminURL := os.Getenv(EnvURL)
	if adminURL == "" {
		t.Skipf("%s is not set; run `task test:integration`", EnvURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		t.Fatalf("open %s: %v", EnvURL, err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	name := "gctest_" + randomSuffix(t)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	d, err := connect(adminURL, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if err := migrate(ctx, d.DB); err != nil {
		t.Fatalf("migrate %s: %v", name, err)
	}
	if _, err := d.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		t.Fatalf("create extension pg_stat_statements: %v", err)
	}
	return d
}

func connect(adminURL, name string) (*DB, error) {
	u, err := url.Parse(adminURL)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvURL, err)
	}
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	password, _ := u.User.Password()
	return &DB{
		DB:       db,
		URL:      u.String(),
		Name:     name,
		Host:     u.Hostname(),
		Port:     u.Port(),
		User:     u.User.Username(),
		Password: password,
	}, nil
}

// migrate runs the same embedded files with goose as the migrator package,
// whose entry point reads the global env.GC instead of taking a *sql.DB.
func migrate(ctx context.Context, db *sql.DB) error {
	schema, err := fs.Sub(migrations.Schema, "schema")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, schema)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

// LatestMigration is the highest version among the embedded migrations.
func LatestMigration() (int64, error) {
	entries, err := fs.ReadDir(migrations.Schema, "schema")
	if err != nil {
		return 0, err
	}
	var latest int64
	for _, e := range entries {
		var v int64
		if _, err := fmt.Sscanf(e.Name(), "%d_", &v); err == nil && strings.HasSuffix(e.Name(), ".sql") && v > latest {
			latest = v
		}
	}
	return latest, nil
}

func randomSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random database name: %v", err)
	}
	return hex.EncodeToString(b)
}
