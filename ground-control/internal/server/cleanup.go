package server

import (
	"context"
	"crypto/rand"
	"log"
	"math/big"
	"time"
)

const (
	cleanupLockID         = 12345
	defaultRetentionDays  = 7
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

func (s *Server) runCleanupWithLock(ctx context.Context, days int) {
	acquired, err := s.tryAcquireAdvisoryLock(ctx, cleanupLockID)
	if err != nil {
		log.Printf("Failed to check advisory lock: %v", err)
		return
	}
	if !acquired {
		return
	}
	defer s.releaseAdvisoryLock(ctx, cleanupLockID)

	// Clean old satellite status
	if err := s.dbQueries.DeleteOldSatelliteStatus(ctx, days); err != nil {
		log.Printf("Status cleanup failed: %v", err)
	}

	// Clean expired sessions (GDPR compliance)
	if err := s.dbQueries.DeleteExpiredSessions(ctx); err != nil {
		log.Printf("Session cleanup failed: %v", err)
	}

	// Clean stale login attempts (GDPR compliance)
	if err := s.dbQueries.DeleteOldLoginAttempts(ctx, int32(days)); err != nil {
		log.Printf("Login attempt cleanup failed: %v", err)
	}

	log.Printf("Cleanup completed (retention: %d days)", days)
}

func (s *Server) tryAcquireAdvisoryLock(ctx context.Context, lockID int) (bool, error) {
	row := s.db.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID)
	var acquired bool
	if err := row.Scan(&acquired); err != nil {
		return false, err
	}
	return acquired, nil
}

func (s *Server) releaseAdvisoryLock(ctx context.Context, lockID int) {
	if _, err := s.db.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockID); err != nil {
		log.Printf("Failed to release advisory lock: %v", err)
	}
}

func NewCleanupConfig() CleanupConfig {
	return CleanupConfig{
		RetentionDays:   defaultRetentionDays,
		CleanupInterval: defaultCleanupInterval,
	}
}
