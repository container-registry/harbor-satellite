package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func fileAudit(path string) config.AuditConfig {
	return config.AuditConfig{Enabled: true, Syslog: config.SyslogAudit{File: config.SyslogAuditFile{Path: path}}}
}

type auditLine struct {
	Outcome string         `json:"outcome"`
	Reason  string         `json:"reason"`
	Details map[string]any `json:"details"`
}

func auditLines(t *testing.T, path string) []auditLine {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var out []auditLine
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var l auditLine
		require.NoError(t, json.Unmarshal([]byte(line[strings.IndexByte(line, '{'):]), &l))
		out = append(out, l)
	}
	return out
}

// Each reload records one config.changed event on the logger that is
// active after it, except a disable, which is recorded on the logger it
// switches off.
func TestReconfigureAuditOnReload(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.log"), filepath.Join(dir, "second.log")
	log := zerolog.Nop()
	audit, err := logger.NewAuditLogger(auditLoggerConfig(config.AuditConfig{}), logger.ComponentSatellite)
	require.NoError(t, err)
	keys := []string{"audit"}

	current := reconfigureAuditOnReload(audit, config.AuditConfig{}, fileAudit(first), keys, &log)
	require.True(t, current.Equal(fileAudit(first)))
	require.True(t, audit.Enabled())
	got := auditLines(t, first)
	require.Len(t, got, 1, "enabling records the change on the new destination")
	require.Equal(t, "success", got[0].Outcome)
	require.Equal(t, "hot_reload", got[0].Details["source"])
	require.Equal(t, []any{"audit"}, got[0].Details["changed_keys"])

	current = reconfigureAuditOnReload(audit, current, fileAudit(second), keys, &log)
	require.True(t, current.Equal(fileAudit(second)))
	require.Len(t, auditLines(t, first), 1, "the old destination gets nothing more")
	require.Len(t, auditLines(t, second), 1)

	current = reconfigureAuditOnReload(audit, current, current, []string{"log_level"}, &log)
	require.Len(t, auditLines(t, second), 2, "a reload that leaves audit alone is still recorded")

	// A regular file as the parent directory fails for root too.
	unwritable := filepath.Join(first, "audit.log")
	kept := reconfigureAuditOnReload(audit, current, fileAudit(unwritable), keys, &log)
	require.True(t, kept.Equal(fileAudit(second)), "a failed reconfigure keeps the previous config")
	got = auditLines(t, second)
	require.Len(t, got, 3, "the failure lands on the logger still in use")
	require.Equal(t, "failure", got[2].Outcome)
	require.Equal(t, string(logger.ReasonReconfigureFailed), got[2].Reason)

	off := reconfigureAuditOnReload(audit, kept, config.AuditConfig{}, keys, &log)
	require.False(t, off.Enabled)
	require.False(t, audit.Enabled())
	got = auditLines(t, second)
	require.Len(t, got, 4, "disabling is recorded before the logger goes away")
	require.Equal(t, "success", got[3].Outcome)
}
