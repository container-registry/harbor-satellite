package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log"
	"math/big"
	"time"
)

const (
	cleanupLockID          = 12345
	defaultRetentionDays   = 7
	defaultCleanupInterval = 24 * time.Hour
)

var advisoryUnlockTimeout = 10 * time.Second

type CleanupConfig struct {
	RetentionDays   int
	CleanupInterval time.Duration
}

func (s *Server) StartCleanupJob(ctx context.Context, cfg CleanupConfig) {
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = defaultRetentionDays
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = defaultCleanupInterval
	}

	ticker := time.NewTicker(cfg.CleanupInterval)
	defer ticker.Stop()

	log.Printf("Status cleanup job started (retention: %d days, interval: %v)", cfg.RetentionDays, cfg.CleanupInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Status cleanup job stopped")
			return
		case <-ticker.C:
			n, err := rand.Int(rand.Reader, big.NewInt(4))
			if err != nil {
				log.Printf("Failed to generate jitter: %v", err)
				n = big.NewInt(0)
			}
			jitter := time.Duration(n.Int64()+1) * time.Minute
			select {
			case <-ctx.Done():
				return
			case <-time.After(jitter):
				s.runCleanupWithLock(ctx, cfg.RetentionDays)
			}
		}
	}
}

func (s *Server) runCleanupWithLock(ctx context.Context, days int) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		log.Printf("Failed to get database connection for advisory lock: %v", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			log.Printf("Failed to close advisory lock connection: %v", err)
		}
	}()

	acquired, err := tryAcquireAdvisoryLock(ctx, conn, cleanupLockID)
	if err != nil {
		log.Printf("Failed to check advisory lock: %v", err)
		discardConn(conn)
		return
	}
	if !acquired {
		return
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), advisoryUnlockTimeout)
		defer cancel()
		if !releaseAdvisoryLock(releaseCtx, conn, cleanupLockID) {
			discardConn(conn)
		}
	}()

	if err := s.dbQueries.DeleteOldSatelliteStatus(ctx, days); err != nil {
		log.Printf("Status cleanup failed: %v", err)
		return
	}

	if err := s.dbQueries.DeleteOrphanedArtifacts(ctx, days); err != nil {
		log.Printf("Orphaned artifacts cleanup failed: %v", err)
	}

	log.Printf("Status cleanup completed (deleted records older than %d days)", days)
}

func tryAcquireAdvisoryLock(ctx context.Context, conn *sql.Conn, lockID int) (bool, error) {
	row := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID)
	var acquired bool
	if err := row.Scan(&acquired); err != nil {
		return false, err
	}
	return acquired, nil
}

func releaseAdvisoryLock(ctx context.Context, conn *sql.Conn, lockID int) bool {
	var released bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", lockID).Scan(&released); err != nil {
		log.Printf("Failed to release advisory lock: %v", err)
		return false
	}
	if !released {
		log.Printf("Failed to release advisory lock %d: not held by this session", lockID)
	}
	return true
}

// discardConn closes the session instead of returning it to the pool, so
// Postgres drops any advisory lock it may still hold.
func discardConn(conn *sql.Conn) {
	if err := conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
		log.Printf("Failed to discard advisory lock connection: %v", err)
	}
}

func NewCleanupConfig() CleanupConfig {
	return CleanupConfig{
		RetentionDays:   defaultRetentionDays,
		CleanupInterval: defaultCleanupInterval,
	}
}
