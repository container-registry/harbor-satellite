package server

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/env"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	"github.com/stretchr/testify/require"
)

// TestLoadAuditConfig covers the happy paths of the env-var -> logger config
// mapping. The invalid-input paths call log.Fatalf (process exit) and so are not
// unit-testable here.
func TestLoadAuditConfig(t *testing.T) {
	t.Run("disabled when AUDIT_LOG_ENABLED is not true", func(t *testing.T) {
		t.Setenv("AUDIT_LOG_ENABLED", "false")
		require.NoError(t, env.LoadGC())
		cfg, err := env.GC.Audit.Config()
		require.NoError(t, err)
		require.Equal(t, auditlog.AuditConfig{}, cfg)
	})

	t.Run("file target resolves defaults", func(t *testing.T) {
		t.Setenv("AUDIT_LOG_ENABLED", "true")
		t.Setenv("AUDIT_SYSLOG_TARGET", "file")
		require.NoError(t, env.LoadGC())
		cfg, err := env.GC.Audit.Config()
		require.NoError(t, err)
		require.True(t, cfg.Enabled)
		require.True(t, cfg.Syslog.Enabled)
		require.Equal(t, auditlog.SyslogTargetFile, cfg.Syslog.Target)
		require.Equal(t, "harbor-audit", cfg.Syslog.Tag)
		require.Equal(t, "./audit.log", cfg.Syslog.File.Path)
		require.Equal(t, 100, cfg.Syslog.File.MaxSizeMB)
		require.Equal(t, 7, cfg.Syslog.File.MaxBackups)
		require.Equal(t, 30, cfg.Syslog.File.MaxAgeDays)
		require.True(t, cfg.Syslog.File.Compress, "compress defaults to true")
	})

	t.Run("compress disabled only on explicit false", func(t *testing.T) {
		t.Setenv("AUDIT_LOG_ENABLED", "true")
		t.Setenv("AUDIT_SYSLOG_TARGET", "file")
		t.Setenv("AUDIT_SYSLOG_FILE_COMPRESS", "false")
		require.NoError(t, env.LoadGC())
		cfg, err := env.GC.Audit.Config()
		require.NoError(t, err)
		require.False(t, cfg.Syslog.File.Compress)
	})

	t.Run("network target carries network and address", func(t *testing.T) {
		t.Setenv("AUDIT_LOG_ENABLED", "true")
		t.Setenv("AUDIT_SYSLOG_TARGET", "network")
		t.Setenv("AUDIT_SYSLOG_NETWORK", "tcp")
		t.Setenv("AUDIT_SYSLOG_ADDRESS", "siem.example:514")
		require.NoError(t, env.LoadGC())
		cfg, err := env.GC.Audit.Config()
		require.NoError(t, err)
		require.Equal(t, auditlog.SyslogTargetNetwork, cfg.Syslog.Target)
		require.Equal(t, "tcp", cfg.Syslog.Network)
		require.Equal(t, "siem.example:514", cfg.Syslog.Address)
	})

	t.Run("daemon target defaults the socket path", func(t *testing.T) {
		t.Setenv("AUDIT_LOG_ENABLED", "true")
		t.Setenv("AUDIT_SYSLOG_TARGET", "daemon")
		require.NoError(t, env.LoadGC())
		cfg, err := env.GC.Audit.Config()
		require.NoError(t, err)
		require.Equal(t, auditlog.SyslogTargetDaemon, cfg.Syslog.Target)
		require.Equal(t, "/dev/log", cfg.Syslog.SocketPath)
	})
}
