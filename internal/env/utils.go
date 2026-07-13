package env

import (
	"fmt"
	"net"
	"net/url"

	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
)

func (d Database) URL() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.Username, d.Password),
		Host:   net.JoinHostPort(d.Host, d.Port),
		Path:   "/" + d.Database,
	}
	query := u.Query()
	query.Set("sslmode", "disable")
	u.RawQuery = query.Encode()
	return u.String()
}

func (h Harbor) Validate() error {
	if h.URL == "" {
		return fmt.Errorf("HARBOR_URL environment variable is not set")
	}
	if h.Username == "" {
		return fmt.Errorf("HARBOR_USERNAME environment variable is not set")
	}
	if h.Password == "" {
		return fmt.Errorf("HARBOR_PASSWORD environment variable is not set")
	}
	return nil
}

func (a Audit) Config() (auditlog.AuditConfig, error) {
	if !a.LogEnabled {
		return auditlog.AuditConfig{}, nil
	}

	syslog, err := a.SyslogConfig()
	if err != nil {
		return auditlog.AuditConfig{}, err
	}

	otel := a.OTelConfig()
	if !syslog.Enabled && !otel.Enabled {
		return auditlog.AuditConfig{}, fmt.Errorf("AUDIT_LOG_ENABLED=true but no transport is enabled: set AUDIT_SYSLOG_ENABLED=true and/or AUDIT_OTEL_ENDPOINT")
	}
	return auditlog.AuditConfig{Enabled: true, Syslog: syslog, OTel: otel}, nil
}

func (a Audit) SyslogConfig() (auditlog.SyslogConfig, error) {
	if !a.SyslogEnabled {
		return auditlog.SyslogConfig{Enabled: false}, nil
	}

	syslog := auditlog.SyslogConfig{
		Enabled:    true,
		Target:     auditlog.SyslogTarget(a.SyslogTarget),
		Tag:        a.SyslogTag,
		SocketPath: a.SyslogSocketPath,
		Network:    a.SyslogNetwork,
		Address:    a.SyslogAddress,
	}

	switch a.SyslogTarget {
	case "file":
		file, err := a.SyslogFileConfig()
		if err != nil {
			return auditlog.SyslogConfig{}, err
		}
		syslog.File = file
	case "network":
		if syslog.Address == "" {
			return auditlog.SyslogConfig{}, fmt.Errorf("AUDIT_SYSLOG_TARGET=network but AUDIT_SYSLOG_ADDRESS is empty")
		}
	case "daemon":
		// SocketPath is populated from envDefault.
	default:
		return auditlog.SyslogConfig{}, fmt.Errorf("AUDIT_SYSLOG_TARGET must be one of daemon|network|file, got %q", a.SyslogTarget)
	}

	return syslog, nil
}

func (a Audit) OTelConfig() auditlog.OTelConfig {
	return auditlog.OTelConfig{
		Enabled:  a.OTelEndpoint != "",
		Endpoint: a.OTelEndpoint,
	}
}

func (a Audit) SyslogFileConfig() (auditlog.SyslogFileConfig, error) {
	path := "./audit.log"
	if a.SyslogFilePath != nil {
		if *a.SyslogFilePath == "" {
			return auditlog.SyslogFileConfig{}, fmt.Errorf("AUDIT_SYSLOG_TARGET=file but AUDIT_SYSLOG_FILE_PATH is empty")
		}
		path = *a.SyslogFilePath
	}

	if a.SyslogFileMaxSizeMB < 1 {
		return auditlog.SyslogFileConfig{}, fmt.Errorf("AUDIT_SYSLOG_FILE_MAX_SIZE_MB must be >= 1, got %d", a.SyslogFileMaxSizeMB)
	}
	if a.SyslogFileMaxBackups < 0 {
		return auditlog.SyslogFileConfig{}, fmt.Errorf("AUDIT_SYSLOG_FILE_MAX_BACKUPS must be >= 0, got %d", a.SyslogFileMaxBackups)
	}
	if a.SyslogFileMaxAgeDays < 0 {
		return auditlog.SyslogFileConfig{}, fmt.Errorf("AUDIT_SYSLOG_FILE_MAX_AGE_DAYS must be >= 0, got %d", a.SyslogFileMaxAgeDays)
	}

	return auditlog.SyslogFileConfig{
		Path:       path,
		MaxSizeMB:  a.SyslogFileMaxSizeMB,
		MaxBackups: a.SyslogFileMaxBackups,
		MaxAgeDays: a.SyslogFileMaxAgeDays,
		Compress:   a.SyslogFileCompress,
	}, nil
}
