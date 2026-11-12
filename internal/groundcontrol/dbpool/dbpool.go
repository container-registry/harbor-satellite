// Package dbpool opens Ground Control's database pools, one per plane
// (plan item 3.2, gate G4): API, fleet and async.
package dbpool

import (
	"context"
	"database/sql"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// DriverName stays lib/pq: it already cancels the server query when the
// context ends (TestLibPQCancelsQueryOnContextCancel), so plan 3.1 needs no
// driver swap.
const DriverName = "postgres"

// PoolConfig is one pool. Zero values keep the database/sql defaults.
type PoolConfig struct {
	// DSN is empty for a fleet or async plane without its own URL; that
	// plane then uses the API pool.
	DSN                    string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetime        time.Duration
	ConnMaxIdleTime        time.Duration
	StatementTimeout       time.Duration
	LockTimeout            time.Duration
	IdleInTxSessionTimeout time.Duration
	ApplicationName        string
}

type Config struct {
	API, Fleet, Async PoolConfig
}

// Pools has one *sql.DB per plane. Fleet and Async are the API pool
// itself unless their config has a DSN.
type Pools struct {
	API, Fleet, Async *sql.DB
}

// ConfigFromEnv takes the API DSN from DB_URL_API or the single DSN, and
// the fleet and async DSNs from DB_URL_FLEET and DB_URL_ASYNC only.
func ConfigFromEnv(db env.Database) (Config, error) {
	return Config{}, notimpl.Err("3.2 pool config")
}

func Open(ctx context.Context, cfg Config) (*Pools, error) {
	return nil, notimpl.Err("3.2 open pools")
}

// Close closes each distinct pool once.
func (p *Pools) Close() error {
	return notimpl.Err("3.2 close pools")
}

// WithRuntimeParams adds statement_timeout, lock_timeout,
// idle_in_transaction_session_timeout (as libpq options) and
// application_name to dsn, so the limits hold without database roles.
func WithRuntimeParams(dsn string, pc PoolConfig) (string, error) {
	return "", notimpl.Err("3.2 runtime params")
}
