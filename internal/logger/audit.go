package logger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
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
//
// The json tags define the canonical wire shape: required fields are always
// present, optional ones carry omitempty so they vanish when unset.
type AuditEvent struct {
	// Required.
	Operation    Operation    `json:"operation"`
	ResourceType ResourceType `json:"resource_type"`
	Outcome      Outcome      `json:"outcome"`

	// Optional. Severity is derived from the event when left empty; the logger
	// resolves it before emit so it is always present in the output.
	Severity    Severity       `json:"severity"`
	Actor       string         `json:"actor,omitempty"`
	ActorType   ActorType      `json:"actor_type,omitempty"`
	SourceIP    string         `json:"source_ip,omitempty"`
	UserAgent   string         `json:"user_agent,omitempty"`
	RequestID   string         `json:"request_id,omitempty"`
	SatelliteID string         `json:"satellite_id,omitempty"`
	Resource    string         `json:"resource,omitempty"`
	Reason      Reason         `json:"reason,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
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

// Record is a fully-resolved audit event: the caller's AuditEvent plus the
// fields the logger fills in at emit time (event_id, timestamp, component,
// event_type, and a resolved severity). Log builds exactly one Record per event
// and hands the same Record to every transport, so one action always produces
// one logical event regardless of how many destinations are attached.
//
// json.Marshal(Record) is the single place the canonical wire JSON is produced;
// every transport serialises through the same Record so the destinations can
// never disagree about what an event looks like.
type Record struct {
	EventID   string    `json:"event_id"`
	Timestamp time.Time `json:"timestamp"`
	Component Component `json:"component"`
	EventType string    `json:"event_type"`
	AuditEvent
}

// Transport is one audit destination behind the logger's seam. Log builds a
// Record once and calls Emit on every attached transport, so adding a new
// destination (syslog, OTel, ...) means implementing this interface and
// registering it in Reconfigure -- Log itself never changes. Emit returns an
// error instead of swallowing it so a failing destination leaves a breadcrumb
// rather than silently dropping events; one transport failing does not stop the
// others.
type Transport interface {
	Emit(r Record) error
	Close() error
}

// AuditConfig controls the audit logger. Enabled is the master switch; the
// destinations are Syslog (local daemon, remote SIEM, or rotated file) and
// OTel (OTLP/HTTP log export), and both may be active at once.
type AuditConfig struct {
	Enabled bool
	Syslog  SyslogConfig
	OTel    OTelConfig
}

// AuditLogger writes structured security events to one or more transports. It is
// safe for concurrent use, and its transports can be swapped at runtime via
// Reconfigure. When disabled it is a no-op so callers never need to nil-check.
type AuditLogger struct {
	mu         sync.RWMutex
	transports []Transport
	enabled    bool
	component  Component
}

// NewAuditLogger builds an AuditLogger from cfg for the given component. With
// Enabled=false the logger is a no-op. When enabled, at least one transport
// (syslog or otel) must be configured and each is verified usable up front; an
// error is returned otherwise, so the caller can fail fast at startup instead of
// advertising audit logging while silently dropping every event.
func NewAuditLogger(cfg AuditConfig, component Component) (*AuditLogger, error) {
	a := &AuditLogger{component: component}
	if err := a.Reconfigure(cfg); err != nil {
		return nil, err
	}

	return a, nil
}

// Reconfigure atomically swaps the logger's transports to match cfg. It is used
// both at construction and on hot reload. The previous transports, if any, are
// closed after the swap. When a new transport cannot be built the error is
// returned and the existing configuration is left untouched.
//
// This is the one place destinations are wired: adding syslog (and later OTel)
// means building the transport in buildTransports.
func (a *AuditLogger) Reconfigure(cfg AuditConfig) error {
	newTransports, err := buildTransports(cfg)
	if err != nil {
		return err
	}

	a.mu.Lock()
	old := a.transports
	a.transports = newTransports
	a.enabled = len(newTransports) > 0
	a.mu.Unlock()

	closeAll(old)

	return nil
}

// buildTransports constructs the transports for cfg, verifying each is usable up
// front. It returns nil when auditing is disabled. When auditing is enabled but
// no transport is configured it returns an error so the caller fails fast at
// startup (and a hot reload that disables every transport is rejected, keeping
// the previous configuration) rather than silently dropping every event.
func buildTransports(cfg AuditConfig) ([]Transport, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	var newTransports []Transport
	if cfg.Syslog.Enabled {
		st, err := newSyslogTransport(cfg.Syslog)
		if err != nil {
			return nil, err
		}
		newTransports = append(newTransports, st)
	}

	if cfg.OTel.Enabled {
		ot, err := newOTelTransport(cfg.OTel)
		if err != nil {
			closeAll(newTransports)
			return nil, err
		}
		newTransports = append(newTransports, ot)
	}

	if len(newTransports) == 0 {
		return nil, errors.New("audit logging is enabled but no transport is configured: enable at least one of syslog or otel")
	}

	return newTransports, nil
}

// closeAll closes every transport in ts, ignoring errors: a transport being
// discarded has nowhere useful to report its close failure.
func closeAll(ts []Transport) {
	for _, t := range ts {
		_ = t.Close()
	}
}

// ensureWritable verifies that path can be created and written to now.
// lumberjack opens its file lazily on first write and silently tolerates write
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
// and a default severity are filled in automatically, and the resulting Record
// is fanned out to every transport. Empty optional fields are omitted. Safe to
// call on a nil or disabled logger.
//
// The transport slice is snapshotted under the lock and the actual emit happens
// after the lock is released, so a slow transport (e.g. syslog over the network)
// never blocks a concurrent Reconfigure. Reconfigure always replaces the slice
// rather than mutating it, so the snapshot stays valid.
func (a *AuditLogger) Log(e AuditEvent) {
	if a == nil {
		return
	}
	a.mu.RLock()
	enabled := a.enabled
	transports := a.transports
	component := a.component
	a.mu.RUnlock()
	if !enabled {
		return
	}

	e = e.withRequiredDefaults()
	e.Severity = e.severity()
	r := Record{
		EventID:    uuid.NewString(),
		Timestamp:  time.Now().UTC(),
		Component:  component,
		EventType:  e.eventType(),
		AuditEvent: e,
	}
	for _, t := range transports {
		if err := t.Emit(r); err != nil {
			logTransportError(component, err)
		}
	}
}

// logTransportError leaves a breadcrumb in the operational log when a transport
// fails. No package-level operational logger is available here (and the linter
// forbids globals), so a minimal stderr logger is built on the rare error path;
// a failing destination must be visible rather than silently dropping events.
func logTransportError(component Component, err error) {
	l := zerolog.New(os.Stderr).With().Timestamp().Logger()
	l.Error().Err(err).Str("component", string(component)).
		Msg("audit transport emit failed")
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

	return &AuditLogger{enabled: false}
}
