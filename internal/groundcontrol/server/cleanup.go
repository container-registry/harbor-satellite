package server

import (
	"context"
	"crypto/rand"
	"log"
	"math/big"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
)

const (
	cleanupLockID          = advisorylock.Cleanup
	membershipLockID       = advisorylock.GroupMembership
	defaultRetentionDays   = 7
	defaultCleanupInterval = 24 * time.Hour
)

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

// runCleanupWithLock runs the cleanup once across replicas. The lock is a
// transaction-level advisory lock taken in the cleanup's own transaction,
// so it is held on the connection doing the work and released at commit or
// rollback; a session lock taken through the pool could be released on a
// different connection and leak.
func (s *Server) runCleanupWithLock(ctx context.Context, days int) {
	db, queries := s.background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Status cleanup: begin transaction: %v", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // After Commit this is a no-op; otherwise the error is the one already logged.
	q := queries.WithTx(tx)

	acquired, err := q.TryAdvisoryXactLock(ctx, cleanupLockID)
	if err != nil {
		log.Printf("Failed to check advisory lock: %v", err)
		return
	}
	if !acquired {
		return
	}

	if err := q.DeleteOldSatelliteStatus(ctx, days); err != nil {
		log.Printf("Status cleanup failed: %v", err)
		return
	}
	if err := q.DeleteOrphanedArtifacts(ctx, days); err != nil {
		log.Printf("Orphaned artifacts cleanup failed: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Status cleanup: commit: %v", err)
		return
	}

	log.Printf("Status cleanup completed (deleted records older than %d days)", days)
}

func NewCleanupConfig() CleanupConfig {
	return CleanupConfig{
		RetentionDays:   defaultRetentionDays,
		CleanupInterval: defaultCleanupInterval,
	}
}
