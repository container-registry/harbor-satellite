package logger

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// parseSyslogLine splits an RFC 5424 line into its header prefix (everything up
// to the JSON MSG) and the JSON body, and unmarshals the body.
func parseSyslogLine(t *testing.T, line string) (header string, body map[string]any) {
	t.Helper()
	line = strings.TrimRight(line, "\n")
	brace := strings.IndexByte(line, '{')
	require.GreaterOrEqual(t, brace, 0, "line should contain a JSON body: %q", line)
	header = strings.TrimSpace(line[:brace])
	require.NoError(t, json.Unmarshal([]byte(line[brace:]), &body))

	return header, body
}

func TestSyslog_FileTargetWritesRFC5424(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.syslog")
	a, err := NewAuditLogger(AuditConfig{
		Enabled: true,
		// No FilePath: only the syslog transport is attached, so the file holds
		// syslog-framed lines, not the raw-JSON transport's output.
		Syslog: SyslogConfig{
			Enabled: true,
			Target:  SyslogTargetFile,
			Tag:     "harbor-audit",
			File:    SyslogFileConfig{Path: path},
		},
	}, ComponentSatellite)
	require.NoError(t, err)
	require.True(t, a.Enabled())

	a.Log(AuditEvent{
		Operation:    OpLogin,
		ResourceType: ResSession,
		Outcome:      OutcomeFailure, // -> warning -> PRI 16*8+4 = 132
		Actor:        "alice",
		Reason:       ReasonBadPassword,
	})

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	line := strings.TrimSpace(string(data))

	header, body := parseSyslogLine(t, line)

	// RFC 5424 header: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID SD
	require.True(t, strings.HasPrefix(header, "<132>1 "), "header was %q", header)
	fields := strings.Fields(header)
	require.Len(t, fields, 7, "header should have 7 space-separated fields: %q", header)

	_, err = time.Parse(rfc5424Time, fields[1])
	require.NoError(t, err, "timestamp field should be RFC 5424 time")
	require.Equal(t, "harbor-audit", fields[3], "APP-NAME should be the tag")
	require.Equal(t, "-", fields[5], "MSGID should be NILVALUE")
	require.Equal(t, "-", fields[6], "STRUCTURED-DATA should be NILVALUE")

	// The body is the same canonical Record JSON as the file transport produces.
	require.Equal(t, "session.login.failure", body["event_type"])
	require.Equal(t, "warning", body["severity"])
	require.Equal(t, "alice", body["actor"])
	require.Equal(t, "bad_password", body["reason"])
}

func TestSyslog_NetworkTargetUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	a, err := NewAuditLogger(AuditConfig{
		Enabled: true,
		Syslog: SyslogConfig{
			Enabled: true,
			Target:  SyslogTargetNetwork,
			Network: "udp",
			Address: pc.LocalAddr().String(),
		},
	}, ComponentGroundControl)
	require.NoError(t, err)

	a.Log(AuditEvent{
		Operation:    OpRegister,
		ResourceType: ResSatellite,
		Outcome:      OutcomeSuccess, // -> info -> PRI 16*8+6 = 134
	})

	require.NoError(t, pc.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 4096)
	n, _, err := pc.ReadFrom(buf)
	require.NoError(t, err)

	header, body := parseSyslogLine(t, string(buf[:n]))
	require.True(t, strings.HasPrefix(header, "<134>1 "), "header was %q", header)
	require.Equal(t, "satellite.register.success", body["event_type"])
	require.Equal(t, "info", body["severity"])
	require.Equal(t, string(ComponentGroundControl), body["component"])
}

func TestSyslog_SeverityCodeMapping(t *testing.T) {
	require.Equal(t, 2, severityCode(SeverityCritical))
	require.Equal(t, 3, severityCode(SeverityError))
	require.Equal(t, 4, severityCode(SeverityWarning))
	require.Equal(t, 6, severityCode(SeverityInfo))
	require.Equal(t, 6, severityCode(Severity("")), "unknown severity defaults to info")
}

func TestSyslog_UnknownTargetErrors(t *testing.T) {
	_, err := NewAuditLogger(AuditConfig{
		Enabled: true,
		Syslog:  SyslogConfig{Enabled: true, Target: SyslogTarget("bogus")},
	}, ComponentSatellite)
	require.Error(t, err)
}

func TestSyslog_DaemonDialErrorSurfaces(t *testing.T) {
	// A socket path that does not exist must fail fast at construction rather
	// than returning a logger that silently drops events.
	_, err := NewAuditLogger(AuditConfig{
		Enabled: true,
		Syslog: SyslogConfig{
			Enabled:    true,
			Target:     SyslogTargetDaemon,
			SocketPath: filepath.Join(t.TempDir(), "nonexistent.sock"),
		},
	}, ComponentSatellite)
	require.Error(t, err)
}
