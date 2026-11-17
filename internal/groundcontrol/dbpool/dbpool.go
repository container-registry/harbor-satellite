// Package dbpool opens Ground Control's database pools, one per plane
// (plan item 3.2, gate G4): API, fleet and async.
package dbpool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	_ "github.com/lib/pq" // registers DriverName
)

// DriverName stays lib/pq: it already cancels the server query when the
// context ends (TestLibPQCancelsQueryOnContextCancel), and ADR-0019 keeps
// every pool except the logical replication connection on it.
const DriverName = "postgres"

const (
	planeAPI   = "groundcontrol-api"
	planeFleet = "groundcontrol-fleet"
	planeAsync = "groundcontrol-async"
)

// PoolConfig is one pool. Zero values keep the database/sql defaults.
type PoolConfig struct {
	// DSN is empty for a fleet or async plane without its own URL or pool
	// settings; that plane then uses the API pool.
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

// ConfigFromEnv takes the API DSN from DB_URL_API or the DB_* settings.
// Fleet and async get a pool of their own when DB_URL_FLEET or
// DB_URL_ASYNC is set, or when any DB_FLEET_* or DB_ASYNC_* pool setting
// is, on the API DSN in the latter case; otherwise they share the API pool.
func ConfigFromEnv(db env.Database) (Config, error) {
	apiDSN := db.URLAPI
	if apiDSN == "" {
		dsn, err := db.ConnectionURL()
		if err != nil {
			return Config{}, err
		}
		apiDSN = dsn
	}
	cfg := Config{
		API:   poolConfig(apiDSN, db.APIPool, planeAPI),
		Fleet: poolConfig(planeDSN(db.URLFleet, db.FleetPool, apiDSN), db.FleetPool, planeFleet),
		Async: poolConfig(planeDSN(db.URLAsync, db.AsyncPool, apiDSN), db.AsyncPool, planeAsync),
	}
	return cfg, nil
}

func planeDSN(url string, pool env.Pool, apiDSN string) string {
	if url != "" {
		return url
	}
	if pool != (env.Pool{}) {
		return apiDSN
	}
	return ""
}

func poolConfig(dsn string, pool env.Pool, applicationName string) PoolConfig {
	return PoolConfig{
		DSN:                    dsn,
		MaxOpenConns:           pool.MaxOpenConns,
		MaxIdleConns:           pool.MaxIdleConns,
		ConnMaxLifetime:        pool.ConnMaxLifetime,
		ConnMaxIdleTime:        pool.ConnMaxIdleTime,
		StatementTimeout:       pool.StatementTimeout,
		LockTimeout:            pool.LockTimeout,
		IdleInTxSessionTimeout: pool.IdleInTxSessionTimeout,
		ApplicationName:        applicationName,
	}
}

// Open opens and pings every pool that has a DSN; the API pool must have one.
func Open(ctx context.Context, cfg Config) (*Pools, error) {
	if cfg.API.DSN == "" {
		return nil, errors.New("dbpool: the API pool needs a DSN")
	}
	api, err := openPool(ctx, cfg.API)
	if err != nil {
		return nil, fmt.Errorf("API pool: %w", err)
	}
	pools := &Pools{API: api, Fleet: api, Async: api}
	if cfg.Fleet.DSN != "" {
		if pools.Fleet, err = openPool(ctx, cfg.Fleet); err != nil {
			return nil, errors.Join(fmt.Errorf("fleet pool: %w", err), pools.Close())
		}
	}
	if cfg.Async.DSN != "" {
		if pools.Async, err = openPool(ctx, cfg.Async); err != nil {
			pools.Async = pools.API
			return nil, errors.Join(fmt.Errorf("async pool: %w", err), pools.Close())
		}
	}
	return pools, nil
}

func openPool(ctx context.Context, pc PoolConfig) (*sql.DB, error) {
	dsn, err := WithRuntimeParams(pc.DSN, pc)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, err
	}
	if pc.MaxOpenConns > 0 {
		db.SetMaxOpenConns(pc.MaxOpenConns)
	}
	if pc.MaxIdleConns > 0 {
		db.SetMaxIdleConns(pc.MaxIdleConns)
	}
	if pc.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(pc.ConnMaxLifetime)
	}
	if pc.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(pc.ConnMaxIdleTime)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("ping: %w", err), db.Close())
	}
	return db, nil
}

// Close closes each distinct pool once.
func (p *Pools) Close() error {
	if p == nil {
		return nil
	}
	var err error
	closed := map[*sql.DB]bool{}
	for _, db := range []*sql.DB{p.API, p.Fleet, p.Async} {
		if db == nil || closed[db] {
			continue
		}
		closed[db] = true
		err = errors.Join(err, db.Close())
	}
	return err
}

// WithRuntimeParams adds statement_timeout, lock_timeout,
// idle_in_transaction_session_timeout (as libpq options) and
// application_name to dsn, so the limits hold without database roles.
// Options the DSN already carries come first, so ours win.
func WithRuntimeParams(dsn string, pc PoolConfig) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("DSN must be a postgres:// URL, got scheme %q", u.Scheme)
	}
	q := u.Query()
	if pc.ApplicationName != "" {
		q.Set("application_name", pc.ApplicationName)
	}
	var options []string
	if existing := strings.TrimSpace(q.Get("options")); existing != "" {
		options = append(options, existing)
	}
	for _, p := range []struct {
		name string
		d    time.Duration
	}{
		{"statement_timeout", pc.StatementTimeout},
		{"lock_timeout", pc.LockTimeout},
		{"idle_in_transaction_session_timeout", pc.IdleInTxSessionTimeout},
	} {
		if p.d > 0 {
			options = append(options, fmt.Sprintf("-c %s=%d", p.name, p.d.Milliseconds()))
		}
	}
	if len(options) > 0 {
		q.Set("options", strings.Join(options, " "))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
