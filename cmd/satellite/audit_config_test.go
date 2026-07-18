package main

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

// TestAuditLoggerConfig covers the on-disk -> logger config mapping used at both
// startup and hot reload, so the two paths stay in agreement.
func TestAuditLoggerConfig(t *testing.T) {
	t.Run("disabled maps to a disabled logger config", func(t *testing.T) {
		out := auditLoggerConfig(config.AuditConfig{Enabled: false})
		require.False(t, out.Enabled)
		require.False(t, out.Syslog.Enabled)
	})

	t.Run("file target resolves target/tag/rotation defaults", func(t *testing.T) {
		out := auditLoggerConfig(config.AuditConfig{
			Enabled: true,
			Syslog:  config.SyslogAudit{File: config.SyslogAuditFile{Path: "/var/log/a.log"}},
		})
		require.True(t, out.Enabled)
		require.True(t, out.Syslog.Enabled)
		require.Equal(t, logger.SyslogTargetFile, out.Syslog.Target, "empty target defaults to file")
		require.Equal(t, "harbor-audit", out.Syslog.Tag, "empty tag defaults")
		require.Equal(t, "/var/log/a.log", out.Syslog.File.Path)
		require.Equal(t, config.DefaultAuditMaxSizeMB, out.Syslog.File.MaxSizeMB)
		require.Equal(t, config.DefaultAuditMaxBackups, out.Syslog.File.MaxBackups)
		require.Equal(t, config.DefaultAuditMaxAgeDays, out.Syslog.File.MaxAgeDays)
		require.True(t, out.Syslog.File.Compress, "compress defaults to true")
	})

	t.Run("network target carries network and address through", func(t *testing.T) {
		out := auditLoggerConfig(config.AuditConfig{
			Enabled: true,
			Syslog: config.SyslogAudit{
				Target:  "network",
				Tag:     "custom-tag",
				Network: "tcp",
				Address: "siem.example:514",
			},
		})
		require.Equal(t, logger.SyslogTargetNetwork, out.Syslog.Target)
		require.Equal(t, "custom-tag", out.Syslog.Tag, "explicit tag preserved")
		require.Equal(t, "tcp", out.Syslog.Network)
		require.Equal(t, "siem.example:514", out.Syslog.Address)
	})

	t.Run("daemon target carries socket path through", func(t *testing.T) {
		out := auditLoggerConfig(config.AuditConfig{
			Enabled: true,
			Syslog:  config.SyslogAudit{Target: "daemon", SocketPath: "/run/custom.sock"},
		})
		require.Equal(t, logger.SyslogTargetDaemon, out.Syslog.Target)
		require.Equal(t, "/run/custom.sock", out.Syslog.SocketPath)
	})
}
