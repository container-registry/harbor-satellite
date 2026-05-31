package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// AuditEventType identifies a security-relevant event recorded in the audit log.
type AuditEventType string

const (
	EventSatelliteAuthFailure  AuditEventType = "satellite.auth.failure"
	EventSatelliteRegistered   AuditEventType = "satellite.registered"
	EventSatelliteDeregistered AuditEventType = "satellite.deregistered"
	EventSatelliteRevoked      AuditEventType = "satellite.revoked"
	EventSatelliteUnrevoked    AuditEventType = "satellite.unrevoked"
	EventUserLoginSuccess      AuditEventType = "user.login.success"
	EventUserLoginFailure      AuditEventType = "user.login.failure"
	EventUserCreated           AuditEventType = "user.created"
	EventUserDeleted           AuditEventType = "user.deleted"
	EventUserPasswordChanged   AuditEventType = "user.password_changed"
	EventPolicyPullBlocked     AuditEventType = "policy.pull_blocked"
	EventConfigChanged         AuditEventType = "config.changed"
)

// AuditConfig controls the audit logger destination and rotation policy.
type AuditConfig struct {
	Enabled    bool
	FilePath   string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
}

// AuditLogger writes structured security events. It is safe for concurrent use,
// and its destination can be swapped at runtime via Reconfigure. When disabled
// it is a no-op so callers never need to nil-check.
type AuditLogger struct {
	mu      sync.RWMutex
	log     *zerolog.Logger
	closer  io.Closer
	enabled bool
}

// NewAuditLogger builds an AuditLogger from cfg. With Enabled=false or an empty
// FilePath the logger is a no-op. When enabled, the destination is verified to
// be writable up front and an error is returned if it is not, so the caller can
// fail fast at startup instead of advertising audit logging while silently
// dropping every event.
func NewAuditLogger(cfg AuditConfig) (*AuditLogger, error) {
	a := &AuditLogger{}
	if err := a.Reconfigure(cfg); err != nil {
		return nil, err
	}
	return a, nil
}

// Reconfigure atomically swaps the logger's destination to match cfg. It is
// used both at construction and on hot reload. The previous destination, if
// any, is closed after the swap. When the new destination is unwritable an
// error is returned and the existing configuration is left untouched.
func (a *AuditLogger) Reconfigure(cfg AuditConfig) error {
	var (
		newLog    zerolog.Logger
		newCloser io.Closer
		enabled   bool
	)

	if !cfg.Enabled || cfg.FilePath == "" {
		newLog = zerolog.New(io.Discard)
	} else {
		if err := ensureWritable(cfg.FilePath); err != nil {
			return fmt.Errorf("audit log destination not writable: %w", err)
		}
		rotator := &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   true,
		}
		newLog = zerolog.New(rotator)
		newCloser = rotator
		enabled = true
	}

	a.mu.Lock()
	old := a.closer
	a.log = &newLog
	a.closer = newCloser
	a.enabled = enabled
	a.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return nil
}

// ensureWritable verifies that path can be created and written to now.
// lumberjack opens its file lazily on first write and zerolog discards write
// errors, so without this probe an unwritable path would look enabled while
// dropping every event. The parent directory is created if missing, mirroring
// what lumberjack itself does on first write.
func ensureWritable(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create audit log directory %q: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log file %q: %w", path, err)
	}
	return f.Close()
}

// Log emits a single audit event. event_id and timestamp are filled in
// automatically. details may be nil.
func (a *AuditLogger) Log(eventType AuditEventType, actor, sourceIP string, details map[string]any) {
	if a == nil {
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.enabled {
		return
	}
	evt := a.log.Log().
		Str("event_id", uuid.NewString()).
		Time("timestamp", time.Now().UTC()).
		Str("event_type", string(eventType)).
		Str("actor", actor).
		Str("source_ip", sourceIP)
	if len(details) > 0 {
		evt = evt.Interface("details", details)
	}
	evt.Send()
}

// Enabled reports whether events will actually be written.
func (a *AuditLogger) Enabled() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.enabled
}

const auditLoggerKey contextKey = "audit_logger"

// WithAuditLogger attaches an AuditLogger to ctx.
func WithAuditLogger(ctx context.Context, a *AuditLogger) context.Context {
	return context.WithValue(ctx, auditLoggerKey, a)
}

// AuditFromContext returns the AuditLogger attached to ctx, or a no-op logger
// if none is set.
func AuditFromContext(ctx context.Context) *AuditLogger {
	if a, ok := ctx.Value(auditLoggerKey).(*AuditLogger); ok && a != nil {
		return a
	}
	l := zerolog.New(io.Discard)
	return &AuditLogger{log: &l, enabled: false}
}
