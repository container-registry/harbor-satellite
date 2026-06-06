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

// The audit event model is a canonical, transport-neutral shape designed to map
// cleanly onto syslog (RFC 5424) and OpenTelemetry without renaming fields
// later. Each event carries eight always-present fields (event_id, timestamp,
// severity, component, event_type, operation, resource_type, outcome) and up to
// nine optional ones. event_type is derived as
// "{resource_type}.{operation}.{outcome}" so consumers never have to parse a
// composite string to recover the parts.

// Component identifies which side emitted the event. It is carried on every
// event so a transport does not have to infer origin from the log file path.
type Component string

const (
	ComponentSatellite     Component = "satellite"
	ComponentGroundControl Component = "ground-control"
)

// Severity prioritises events for a SIEM without parsing event_type. The four
// levels map directly onto syslog PRI (critical=2, error=3, warning=4, info=6)
// and onto OTel SeverityText/SeverityNumber.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Operation is the verb of an audited action.
type Operation string

const (
	OpLogin          Operation = "login"
	OpCreate         Operation = "create"
	OpDelete         Operation = "delete"
	OpUpdate         Operation = "update"
	OpRegister       Operation = "register"
	OpDeregister     Operation = "deregister"
	OpPasswordChange Operation = "password_change"
	OpAuth           Operation = "auth"
	OpRevoke         Operation = "revoke"
	OpUnrevoke       Operation = "unrevoke"
)

// ResourceType is the noun an operation acts on.
type ResourceType string

const (
	ResUser      ResourceType = "user"
	ResSatellite ResourceType = "satellite"
	ResConfig    ResourceType = "config"
	ResSession   ResourceType = "session"
	ResPolicy    ResourceType = "policy"
	ResRobot     ResourceType = "robot"
)

// Outcome records whether the action succeeded.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

// ActorType distinguishes the kind of principal that triggered the event.
type ActorType string

const (
	ActorUser      ActorType = "user"
	ActorRobot     ActorType = "robot"
	ActorSatellite ActorType = "satellite"
	ActorAnonymous ActorType = "anonymous"
	ActorSystem    ActorType = "system"
)

// Reason is a low-cardinality failure code suitable for alerting and
// aggregation. It maps onto OTel error.type. Free-form failure text belongs in
// Details, never here.
type Reason string

const (
	ReasonInvalidCredentials     Reason = "invalid_credentials"
	ReasonMissingCredentials     Reason = "missing_credentials"
	ReasonAccountLocked          Reason = "account_locked"
	ReasonUnknownUser            Reason = "unknown_user"
	ReasonBadPassword            Reason = "bad_password"
	ReasonInvalidToken           Reason = "invalid_token"
	ReasonTokenExpired           Reason = "token_expired"
	ReasonMissingSpiffeIdentity  Reason = "missing_spiffe_identity"
	ReasonInvalidSpiffeID        Reason = "invalid_spiffe_id"
	ReasonInvalidStateAuthConfig Reason = "invalid_state_auth_config"
	ReasonRegistrationFailed     Reason = "registration_failed"
	ReasonReconfigureFailed      Reason = "reconfigure_failed"
	ReasonForbidden              Reason = "forbidden"
	ReasonNotFound               Reason = "not_found"
	ReasonRateLimited            Reason = "rate_limited"
)

// AuditEvent is a single security-relevant event. Callers populate the semantic
// fields (Operation, ResourceType, Outcome are required); the logger fills
// event_id, timestamp, and component, and derives event_type and a default
// severity at emit time. Empty optional fields are omitted from the output.
type AuditEvent struct {
	// Required.
	Operation    Operation
	ResourceType ResourceType
	Outcome      Outcome

	// Optional. Severity is derived from the event when left empty.
	Severity    Severity
	Actor       string
	ActorType   ActorType
	SourceIP    string
	UserAgent   string
	RequestID   string
	SatelliteID string
	Resource    string
	Reason      Reason
	Details     map[string]any
}

// eventType derives the canonical "{resource_type}.{operation}.{outcome}" name.
func (e AuditEvent) eventType() string {
	return fmt.Sprintf("%s.%s.%s", e.ResourceType, e.Operation, e.Outcome)
}

// severity returns the caller-set severity, or a default derived from the
// outcome: failures are warnings, everything else is informational. Callers can
// override with SeverityError / SeverityCritical for genuinely severe events.
func (e AuditEvent) severity() Severity {
	if e.Severity != "" {
		return e.Severity
	}
	if e.Outcome == OutcomeFailure {
		return SeverityWarning
	}
	return SeverityInfo
}

// fieldUnknown is substituted for an empty required field so a caller that
// forgets one still produces a well-formed event_type.
const fieldUnknown = "unknown"

// withRequiredDefaults replaces any empty required field with fieldUnknown so
// event_type always has three non-empty segments (e.g. "user.unknown.success")
// instead of one with an empty segment ("user..success"). A malformed segment
// would otherwise become a broken syslog MSGID / OTel event.name once those
// transports derive identifiers from event_type; the sentinel makes the coding
// bug visible in the SIEM rather than silently corrupting the canonical shape.
func (e AuditEvent) withRequiredDefaults() AuditEvent {
	if e.Operation == "" {
		e.Operation = Operation(fieldUnknown)
	}
	if e.ResourceType == "" {
		e.ResourceType = ResourceType(fieldUnknown)
	}
	if e.Outcome == "" {
		e.Outcome = Outcome(fieldUnknown)
	}
	return e
}

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
	mu        sync.RWMutex
	log       *zerolog.Logger
	closer    io.Closer
	enabled   bool
	component Component
}

// NewAuditLogger builds an AuditLogger from cfg for the given component. With
// Enabled=false or an empty FilePath the logger is a no-op. When enabled, the
// destination is verified to be writable up front and an error is returned if
// it is not, so the caller can fail fast at startup instead of advertising
// audit logging while silently dropping every event.
func NewAuditLogger(cfg AuditConfig, component Component) (*AuditLogger, error) {
	a := &AuditLogger{component: component}
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
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create audit log directory %q: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log file %q: %w", path, err)
	}
	return f.Close()
}

// Log emits a single audit event. event_id, timestamp, component, event_type
// and a default severity are filled in automatically. Empty optional fields are
// omitted. Safe to call on a nil or disabled logger.
func (a *AuditLogger) Log(e AuditEvent) {
	if a == nil {
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.enabled {
		return
	}
	e = e.withRequiredDefaults()
	evt := a.log.Log().
		Str("event_id", uuid.NewString()).
		Str("timestamp", time.Now().UTC().Format(time.RFC3339Nano)).
		Str("severity", string(e.severity())).
		Str("component", string(a.component)).
		Str("event_type", e.eventType()).
		Str("operation", string(e.Operation)).
		Str("resource_type", string(e.ResourceType)).
		Str("outcome", string(e.Outcome))
	// Optional string fields are emitted only when set. Kept as a table so the
	// emit path stays low-complexity as fields are added.
	for _, f := range []struct{ key, val string }{
		{"actor", e.Actor},
		{"actor_type", string(e.ActorType)},
		{"source_ip", e.SourceIP},
		{"user_agent", e.UserAgent},
		{"request_id", e.RequestID},
		{"satellite_id", e.SatelliteID},
		{"resource", e.Resource},
		{"reason", string(e.Reason)},
	} {
		if f.val != "" {
			evt = evt.Str(f.key, f.val)
		}
	}
	if len(e.Details) > 0 {
		evt = evt.Interface("details", e.Details)
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

type contextKey string

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
