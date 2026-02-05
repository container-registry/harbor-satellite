package logger

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// AuditEvent represents a security-relevant audit log entry.
type AuditEvent struct {
	EventID   string                 `json:"event_id"`
	Timestamp time.Time              `json:"timestamp"`
	EventType string                 `json:"event_type"`
	Actor     string                 `json:"actor,omitempty"`
	SourceIP  string                 `json:"source_ip,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

var (
	auditLogger     *zerolog.Logger
	auditLoggerOnce sync.Once
)

// InitAuditLogger configures the global audit logger.
// If writer is nil, audit logging is disabled.
func InitAuditLogger(writer io.Writer) {
	if writer == nil {
		return
	}

	auditLoggerOnce.Do(func() {
		l := zerolog.New(writer).With().Timestamp().Logger()
		auditLogger = &l
	})
}

// LogAuditEvent writes a structured audit event if the audit logger is configured.
func LogAuditEvent(eventType, actor, sourceIP string, details map[string]interface{}) {
	if auditLogger == nil {
		return
	}

	event := AuditEvent{
		EventID:   uuid.NewString(),
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		Actor:     actor,
		SourceIP:  sourceIP,
		Details:   details,
	}

	e := auditLogger.Info().
		Str("event_id", event.EventID).
		Time("timestamp", event.Timestamp).
		Str("event_type", event.EventType)

	if event.Actor != "" {
		e = e.Str("actor", event.Actor)
	}
	if event.SourceIP != "" {
		e = e.Str("source_ip", event.SourceIP)
	}
	if len(event.Details) > 0 {
		e = e.Fields(event.Details)
	}

	e.Msg("")
}

// NewFileAuditWriter returns a basic file writer for audit logs.
// Rotation is expected to be handled by external logrotate where used.
func NewFileAuditWriter(path string) (io.Writer, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return f, nil
}

