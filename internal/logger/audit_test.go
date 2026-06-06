package logger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readAuditLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		out = append(out, entry)
	}
	return out
}

func TestAuditLogger_WritesStructuredEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{
		Enabled:    true,
		FilePath:   path,
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
	}, ComponentSatellite)
	require.NoError(t, err)
	require.True(t, a.Enabled())

	a.Log(AuditEvent{
		Operation:    OpLogin,
		ResourceType: ResSession,
		Outcome:      OutcomeFailure,
		Actor:        "alice",
		ActorType:    ActorUser,
		SourceIP:     "10.0.0.5",
		Reason:       ReasonBadPassword,
		Details:      map[string]any{"attempt": 3},
	})

	entries := readAuditLines(t, path)
	require.Len(t, entries, 1)
	e := entries[0]

	// Mandatory fields are always present.
	require.NotEmpty(t, e["event_id"])
	require.NotEmpty(t, e["timestamp"])
	require.Equal(t, string(ComponentSatellite), e["component"])
	require.Equal(t, "login", e["operation"])
	require.Equal(t, "session", e["resource_type"])
	require.Equal(t, "failure", e["outcome"])

	// event_type is derived as {resource_type}.{operation}.{outcome}.
	require.Equal(t, "session.login.failure", e["event_type"])

	// Failures default to warning severity.
	require.Equal(t, "warning", e["severity"])

	// Optional fields carried through.
	require.Equal(t, "alice", e["actor"])
	require.Equal(t, "user", e["actor_type"])
	require.Equal(t, "10.0.0.5", e["source_ip"])
	require.Equal(t, "bad_password", e["reason"])

	_, err = time.Parse(time.RFC3339Nano, e["timestamp"].(string))
	require.NoError(t, err)

	details, ok := e["details"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 3, details["attempt"])
}

func TestAuditLogger_DerivesDefaultSeverityAndOmitsEmptyFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path}, ComponentSatellite)
	require.NoError(t, err)

	a.Log(AuditEvent{
		Operation:    OpRegister,
		ResourceType: ResSatellite,
		Outcome:      OutcomeSuccess,
	})

	e := readAuditLines(t, path)[0]
	require.Equal(t, "satellite.register.success", e["event_type"])
	require.Equal(t, "info", e["severity"], "successes default to info")

	// Unset optional fields must not appear in the output at all.
	for _, k := range []string{"actor", "actor_type", "source_ip", "user_agent", "request_id", "satellite_id", "resource", "reason", "details"} {
		_, present := e[k]
		require.Falsef(t, present, "optional field %q should be omitted when empty", k)
	}
}

func TestAuditLogger_EmptyRequiredFieldsGetSentinel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path}, ComponentSatellite)
	require.NoError(t, err)

	// A caller that forgets the required fields must not produce an event_type
	// with empty segments ("user..success" / "..") that would become a broken
	// syslog MSGID or OTel event.name. The sentinel keeps it well-formed.
	a.Log(AuditEvent{})

	e := readAuditLines(t, path)[0]
	require.Equal(t, "unknown", e["operation"])
	require.Equal(t, "unknown", e["resource_type"])
	require.Equal(t, "unknown", e["outcome"])
	require.Equal(t, "unknown.unknown.unknown", e["event_type"])
}

func TestAuditLogger_SeverityOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path}, ComponentSatellite)
	require.NoError(t, err)

	a.Log(AuditEvent{
		Operation:    OpDeregister,
		ResourceType: ResSatellite,
		Outcome:      OutcomeSuccess,
		Severity:     SeverityCritical,
	})

	e := readAuditLines(t, path)[0]
	require.Equal(t, "critical", e["severity"], "explicit severity overrides the derived default")
}

func TestAuditLogger_UniqueEventIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path}, ComponentSatellite)
	require.NoError(t, err)

	for range 5 {
		a.Log(AuditEvent{Operation: OpRegister, ResourceType: ResSatellite, Outcome: OutcomeSuccess})
	}

	entries := readAuditLines(t, path)
	require.Len(t, entries, 5)

	seen := make(map[string]struct{})
	for _, e := range entries {
		id := e["event_id"].(string)
		_, dup := seen[id]
		require.False(t, dup, "duplicate event_id %s", id)
		seen[id] = struct{}{}
	}
}

func TestAuditLogger_DisabledIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: false, FilePath: path}, ComponentSatellite)
	require.NoError(t, err)
	require.False(t, a.Enabled())

	a.Log(AuditEvent{Operation: OpLogin, ResourceType: ResSession, Outcome: OutcomeFailure})

	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "no file should be created when disabled")
}

func TestAuditLogger_EmptyPathIsNoOp(t *testing.T) {
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: ""}, ComponentSatellite)
	require.NoError(t, err)
	require.False(t, a.Enabled())
	a.Log(AuditEvent{Operation: OpLogin, ResourceType: ResSession, Outcome: OutcomeSuccess})
}

func TestAuditLogger_UnwritableDestinationErrors(t *testing.T) {
	// Use a regular file in place of a parent directory so MkdirAll fails with
	// ENOTDIR. Construction must surface an error rather than returning a logger
	// that silently drops every event.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	a, err := NewAuditLogger(AuditConfig{
		Enabled:  true,
		FilePath: filepath.Join(blocker, "audit.log"),
	}, ComponentSatellite)
	require.Error(t, err)
	require.Nil(t, a)
}

func TestAuditLogger_ReconfigureSwapsDestination(t *testing.T) {
	// Start disabled, then enable via Reconfigure (the hot-reload path).
	a, err := NewAuditLogger(AuditConfig{Enabled: false}, ComponentSatellite)
	require.NoError(t, err)
	require.False(t, a.Enabled())

	path := filepath.Join(t.TempDir(), "audit.log")
	require.NoError(t, a.Reconfigure(AuditConfig{Enabled: true, FilePath: path}))
	require.True(t, a.Enabled())

	a.Log(AuditEvent{Operation: OpLogin, ResourceType: ResSession, Outcome: OutcomeSuccess})
	require.Len(t, readAuditLines(t, path), 1)

	// Disable again; further events must not be written.
	require.NoError(t, a.Reconfigure(AuditConfig{Enabled: false}))
	require.False(t, a.Enabled())
	a.Log(AuditEvent{Operation: OpLogin, ResourceType: ResSession, Outcome: OutcomeSuccess})
	require.Len(t, readAuditLines(t, path), 1)
}

func TestAuditLogger_NilReceiverSafe(t *testing.T) {
	var a *AuditLogger
	require.NotPanics(t, func() {
		a.Log(AuditEvent{Operation: OpLogin, ResourceType: ResSession, Outcome: OutcomeSuccess})
	})
	require.False(t, a.Enabled())
}

func TestAuditFromContext_DefaultsToNoOp(t *testing.T) {
	ctx := context.Background()
	a := AuditFromContext(ctx)
	require.NotNil(t, a)
	require.False(t, a.Enabled())
}

func TestAuditFromContext_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	a, err := NewAuditLogger(AuditConfig{Enabled: true, FilePath: path}, ComponentSatellite)
	require.NoError(t, err)

	ctx := WithAuditLogger(context.Background(), a)
	got := AuditFromContext(ctx)
	require.Same(t, a, got)
}
