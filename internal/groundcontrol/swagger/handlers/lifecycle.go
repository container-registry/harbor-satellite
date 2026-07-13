package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/container-registry/harbor-satellite/internal/env"
	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
)

const (
	systemAdminUsername  = "admin"
	cleanupLockID        = 12345
	statusRetentionDays  = 7
	statusCleanupPeriod  = 24 * time.Hour
	cleanupUnlockTimeout = 2 * time.Second
)

// Initialize establishes the shared handler service, verifies database
// connectivity, and bootstraps the one system administrator. Migrations must be
// run before this function.
func Initialize(ctx context.Context) error {
	svc, err := getService()
	if err != nil {
		return fmt.Errorf("initialize Ground Control service: %w", err)
	}
	if err := svc.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping Ground Control database: %w", err)
	}
	if err := svc.bootstrapSystemAdmin(ctx); err != nil {
		return fmt.Errorf("bootstrap system administrator: %w", err)
	}
	return nil
}

func (s *service) bootstrapSystemAdmin(ctx context.Context) error {
	exists, err := s.queries.SystemAdminExists(ctx)
	if err != nil {
		return fmt.Errorf("check system administrator existence: %w", err)
	}
	if exists {
		log.Println("System admin already exists, skipping bootstrap")
		return nil
	}

	password := env.GC.Server.AdminPassword
	if password == "" {
		return errors.New("ADMIN_PASSWORD environment variable is required for initial setup")
	}
	if err := s.passwordPolicy.Validate(password); err != nil {
		return fmt.Errorf("ADMIN_PASSWORD invalid: %w", err)
	}

	hash, err := gcauth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash system administrator password: %w", err)
	}

	user, err := s.queries.CreateUser(ctx, database.CreateUserParams{
		Username:     systemAdminUsername,
		PasswordHash: hash,
		Role:         roleSystemAdmin,
	})
	if err != nil {
		// Another instance may have completed bootstrap after our existence
		// check. Treat that race as success when the role now exists.
		exists, checkErr := s.queries.SystemAdminExists(ctx)
		if checkErr == nil && exists {
			log.Println("System admin created by another instance")
			return nil
		}
		return fmt.Errorf("create system administrator: %w", err)
	}

	s.auditEvent(nil, auditlog.AuditEvent{
		Operation:    auditlog.OpCreate,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        "ground-control",
		ActorType:    auditlog.ActorSystem,
		Resource:     user.Username,
		Details:      map[string]any{"role": user.Role, "flow": "bootstrap"},
	})
	log.Println("System admin user created successfully")
	return nil
}

// StartBackgroundJobs starts the status-retention cleanup worker once.
func StartBackgroundJobs() error {
	svc, err := getService()
	if err != nil {
		return fmt.Errorf("initialize background jobs: %w", err)
	}

	svc.lifecycleMu.Lock()
	defer svc.lifecycleMu.Unlock()
	if svc.cleanupCancel != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc.cleanupCancel = cancel
	svc.cleanupWG.Go(func() {
		defer svc.cleanupWG.Done()
		svc.startCleanupJob(ctx, statusRetentionDays, statusCleanupPeriod)
	})
	return nil
}

// StopBackgroundJobs begins graceful application shutdown without closing
// resources that in-flight HTTP handlers may still be using.
func StopBackgroundJobs() {
	if serviceInst == nil {
		return
	}
	serviceInst.stopBackgroundJobs()
}

func (s *service) stopBackgroundJobs() {
	s.lifecycleMu.Lock()
	cancel := s.cleanupCancel
	s.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Shutdown stops background workers and closes the rate limiter, audit
// transports, and database after the HTTP server has drained. It is idempotent.
func Shutdown(ctx context.Context) error {
	if serviceInst == nil {
		return nil
	}
	return serviceInst.shutdown(ctx)
}

func (s *service) shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.stopBackgroundJobs()

		cleanupDone := make(chan struct{})
		go func() {
			s.cleanupWG.Wait()
			close(cleanupDone)
		}()

		var shutdownErrors []error
		select {
		case <-cleanupDone:
		case <-ctx.Done():
			shutdownErrors = append(shutdownErrors, fmt.Errorf("wait for cleanup worker: %w", ctx.Err()))
		}

		if s.rateLimiter != nil {
			s.rateLimiter.Close()
		}
		if err := s.audit.Close(); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("close audit logger: %w", err))
		}
		if err := s.db.Close(); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("close database: %w", err))
		}
		s.shutdownErr = errors.Join(shutdownErrors...)
	})
	return s.shutdownErr
}

func (s *service) startCleanupJob(ctx context.Context, retentionDays int, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Status cleanup job started (retention: %d days, interval: %v)", retentionDays, interval)
	defer log.Println("Status cleanup job stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jitterValue, err := rand.Int(rand.Reader, big.NewInt(4))
			if err != nil {
				log.Printf("Failed to generate cleanup jitter: %v", err)
				jitterValue = big.NewInt(0)
			}
			jitter := time.Duration(jitterValue.Int64()+1) * time.Minute
			timer := time.NewTimer(jitter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				s.runCleanupWithLock(ctx, retentionDays)
			}
		}
	}
}

func (s *service) runCleanupWithLock(ctx context.Context, retentionDays int) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		log.Printf("Failed to reserve cleanup database connection: %v", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close cleanup database connection: %v", err)
		}
	}()

	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", cleanupLockID).Scan(&acquired); err != nil {
		log.Printf("Failed to check cleanup advisory lock: %v", err)
		return
	}
	if !acquired {
		return
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), cleanupUnlockTimeout)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", cleanupLockID); err != nil {
			log.Printf("Failed to release cleanup advisory lock: %v", err)
		}
	}()

	queries := database.New(conn)
	if err := queries.DeleteOldSatelliteStatus(ctx, retentionDays); err != nil {
		log.Printf("Status cleanup failed: %v", err)
		return
	}
	if err := queries.DeleteOrphanedArtifacts(ctx, retentionDays); err != nil {
		log.Printf("Orphaned artifacts cleanup failed: %v", err)
	}
	log.Printf("Status cleanup completed (deleted records older than %d days)", retentionDays)
}

var _ database.DBTX = (*sql.Conn)(nil)
