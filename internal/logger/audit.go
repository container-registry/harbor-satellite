package logger

import (
	"context"
	"io"
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

// AuditLogger writes structured security events. When disabled it is a no-op
// so callers never need to nil-check.
type AuditLogger struct {
	log     *zerolog.Logger
	enabled bool
}

// NewAuditLogger builds an AuditLogger from cfg. With Enabled=false or an
// empty FilePath, the returned logger discards all events.
func NewAuditLogger(cfg AuditConfig) *AuditLogger {
	if !cfg.Enabled || cfg.FilePath == "" {
		l := zerolog.New(io.Discard)
		return &AuditLogger{log: &l, enabled: false}
	}

	rotator := &lumberjack.Logger{
		Filename:   cfg.FilePath,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   true,
	}

	l := zerolog.New(rotator)
	return &AuditLogger{log: &l, enabled: true}
}

// Log emits a single audit event. event_id and timestamp are filled in
// automatically. details may be nil.
func (a *AuditLogger) Log(eventType AuditEventType, actor, sourceIP string, details map[string]any) {
	if a == nil || !a.enabled {
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
	return a != nil && a.enabled
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
