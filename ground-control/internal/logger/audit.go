package logger

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

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

// getAuditLogger lazily initializes the audit logger.
// Configuration is controlled via environment variable AUDIT_LOG_PATH.
func getAuditLogger() *zerolog.Logger {
	auditLoggerOnce.Do(func() {
		path := os.Getenv("AUDIT_LOG_PATH")
		var w *os.File
		if path == "" {
			w = os.Stdout
		} else {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return
			}
			w = f
		}

		l := zerolog.New(w).With().Timestamp().Logger()
		auditLogger = &l
	})

	return auditLogger
}

func LogEvent(_ context.Context, eventType, actor, sourceIP string, details map[string]interface{}) {
	l := getAuditLogger()
	if l == nil {
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

	e := l.Info().
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

// ClientIP extracts the best-effort client IP for audit logging.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}

	// Prefer X-Forwarded-For if present
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

