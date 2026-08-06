package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/container-registry/harbor-satellite/internal/satellite/registry"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// ValidateAndEnforceDefaults validates and normalizes the given config.
// It applies default values where required, verifies URLs, cron expressions,
// and handles the logic for bring-your-own-registry vs default registry setup.
// Returns warnings for any defaulted or ignored fields and a fatal error for critical misconfigurations.
func ValidateAndEnforceDefaults(config *Config, defaultGroundControlURL string) (*Config, []string, error) {
	if config == nil {
		config = &Config{}
	}

	var warnings []string

	// Environment variable/CLI flag always takes precedence over config file
	if defaultGroundControlURL != "" {
		if config.AppConfig.GroundControlURL != "" && string(config.AppConfig.GroundControlURL) != defaultGroundControlURL {
			warnings = append(warnings, fmt.Sprintf(
				"ground_control_url from env/CLI (%s) takes precedence over config file (%s)",
				defaultGroundControlURL, config.AppConfig.GroundControlURL,
			))
		}
		config.AppConfig.GroundControlURL = URL(defaultGroundControlURL)
	}

	if _, err := url.ParseRequestURI(string(config.AppConfig.GroundControlURL)); err != nil {
		return nil, nil, fmt.Errorf("invalid URL provided for ground_control_url: %w", err)
	}

	warnings = append(warnings, validateAndEnforceLogLevel(config)...)

	// Check for USE_UNSECURE environment variable
	if useUnsecure := os.Getenv("USE_UNSECURE"); useUnsecure != "" {
		config.AppConfig.UseUnsecure = strings.ToLower(useUnsecure) == "true" || useUnsecure == "1"
	}

	bringOwnRegistry := config.AppConfig.BringOwnRegistry

	if bringOwnRegistry {
		borWarnings, borErr := validateBringOwnRegistry(config)
		warnings = append(warnings, borWarnings...)
		if borErr != nil {
			return nil, warnings, borErr
		}
	}

	zotWarnings, zotErr := validateAndEnforceZotConfig(config, bringOwnRegistry)
	warnings = append(warnings, zotWarnings...)
	if zotErr != nil {
		return nil, warnings, zotErr
	}

	warnings = append(warnings, validateAndEnforceCronSchedules(config)...)

	tlsWarnings, tlsErr := validateTLSConfig(&config.AppConfig.TLS)
	warnings = append(warnings, tlsWarnings...)
	if tlsErr != nil {
		return nil, warnings, tlsErr
	}

	fbWarnings, fbErr := validateRegistryFallbackConfig(config)
	warnings = append(warnings, fbWarnings...)
	if fbErr != nil {
		return nil, warnings, fbErr
	}

	warnings = append(warnings, validateAndEnforceAuditConfig(config)...)

	return config, warnings, nil
}

// validateAndEnforceAuditConfig fills in defaults for the audit syslog transport
// when audit logging is enabled, applying only the defaults relevant to the
// chosen target.
func validateAndEnforceAuditConfig(config *Config) []string {
	a := &config.AppConfig.Audit
	if !a.Enabled {
		return nil
	}
	var warnings []string
	if a.Syslog.EnabledOrDefault() {
		warnings = append(warnings, enforceSyslogConfig(&a.Syslog)...)
	}
	warnings = append(warnings, validateOtelConfig(&a.Otel)...)
	if !a.Syslog.EnabledOrDefault() && !a.Otel.Enabled {
		warnings = append(warnings, "audit.enabled is true but no transport is enabled (audit.syslog.enabled=false and audit.otel.enabled=false); the audit logger will fail to start")
	}

	return warnings
}

// validateOtelConfig warns when the otel transport is enabled without an
// endpoint, mirroring the network-target warning below.
func validateOtelConfig(o *OtelAudit) []string {
	if o.Enabled && o.Endpoint == "" {
		return []string{"audit.otel.enabled but audit.otel.endpoint is empty; the audit logger will fail to start"}
	}

	return nil
}

// enforceSyslogConfig defaults the common fields and dispatches to the
// per-target enforcement so each function stays low in complexity.
func enforceSyslogConfig(s *SyslogAudit) []string {
	if s.Target == "" {
		s.Target = DefaultAuditSyslogTarget
	}
	if s.Tag == "" {
		s.Tag = DefaultAuditSyslogTag
	}

	switch s.Target {
	case "daemon":
		return enforceDaemonTarget(s)
	case "network":
		return validateNetworkTarget(s)
	case "file":
		return enforceFileTarget(s)
	default:
		return enforceInvalidTarget(s)
	}
}

// enforceFileTarget defaults the file path and rotation settings.
func enforceFileTarget(s *SyslogAudit) []string {
	var warnings []string
	if s.File.Path == "" {
		warnings = append(warnings, fmt.Sprintf("audit.syslog.file.path empty, defaulting to %s", DefaultAuditFilePath))
		s.File.Path = DefaultAuditFilePath
	}

	return append(warnings, enforceAuditRotation(&s.File)...)
}

// enforceDaemonTarget defaults the local syslog socket path.
func enforceDaemonTarget(s *SyslogAudit) []string {
	if s.SocketPath == "" {
		s.SocketPath = DefaultAuditSyslogSocket
	}

	return nil
}

// validateNetworkTarget defaults the network and warns on a missing address.
func validateNetworkTarget(s *SyslogAudit) []string {
	var warnings []string
	if s.Network == "" {
		s.Network = "udp"
		warnings = append(warnings, "audit.syslog.network empty, defaulting to udp")
	}
	if s.Address == "" {
		warnings = append(warnings, "audit.syslog.target=network but audit.syslog.address is empty; the audit logger will fail to start")
	}

	return warnings
}

// enforceInvalidTarget falls back to the file target with a warning.
func enforceInvalidTarget(s *SyslogAudit) []string {
	warnings := []string{fmt.Sprintf("audit.syslog.target %q is invalid (expected daemon|network|file), defaulting to %s", s.Target, DefaultAuditSyslogTarget)}
	s.Target = DefaultAuditSyslogTarget

	return append(warnings, enforceFileTarget(s)...)
}

// enforceAuditRotation fills in defaults for the audit log rotation fields. An
// omitted field (nil) defaults silently, matching Ground Control's env-var
// defaults. For MaxBackups/MaxAgeDays an explicit 0 is a deliberate "retain
// everything" (lumberjack semantics) and is preserved; only genuine negatives
// (and a non-positive MaxSizeMB) warn and default.
func enforceAuditRotation(f *SyslogAuditFile) []string {
	var warnings []string
	if f.MaxSizeMB == nil {
		v := DefaultAuditMaxSizeMB
		f.MaxSizeMB = &v
	} else if *f.MaxSizeMB <= 0 {
		warnings = append(warnings, fmt.Sprintf("audit.syslog.file.max_size_mb must be > 0, defaulting to %d", DefaultAuditMaxSizeMB))
		*f.MaxSizeMB = DefaultAuditMaxSizeMB
	}
	if f.MaxBackups == nil {
		v := DefaultAuditMaxBackups
		f.MaxBackups = &v
	} else if *f.MaxBackups < 0 {
		warnings = append(warnings, fmt.Sprintf("audit.syslog.file.max_backups must be >= 0, defaulting to %d", DefaultAuditMaxBackups))
		*f.MaxBackups = DefaultAuditMaxBackups
	}
	if f.MaxAgeDays == nil {
		v := DefaultAuditMaxAgeDays
		f.MaxAgeDays = &v
	} else if *f.MaxAgeDays < 0 {
		warnings = append(warnings, fmt.Sprintf("audit.syslog.file.max_age_days must be >= 0, defaulting to %d", DefaultAuditMaxAgeDays))
		*f.MaxAgeDays = DefaultAuditMaxAgeDays
	}

	return warnings
}

// isValidCronExpression checks the validity of a cron expression.
func isValidCronExpression(cronExpression string) bool {
	if _, err := cron.ParseStandard(cronExpression); err != nil {
		return false
	}

	return true
}

// validateAndEnforceLogLevel validates log level and defaults to info if invalid.
func validateAndEnforceLogLevel(config *Config) []string {
	var warnings []string
	if config.AppConfig.LogLevel == "" {
		config.AppConfig.LogLevel = zerolog.LevelInfoValue
	} else if !validLogLevels[strings.ToLower(config.AppConfig.LogLevel)] {
		warnings = append(warnings, fmt.Sprintf(
			"invalid log_level '%s' provided. Valid options are: info, debug, panic, error, warn, fatal. Defaulting to 'info'.",
			config.AppConfig.LogLevel,
		))
		config.AppConfig.LogLevel = zerolog.LevelInfoValue
	}

	return warnings
}

// validateBringOwnRegistry validates custom registry configuration.
func validateBringOwnRegistry(config *Config) ([]string, error) {
	var warnings []string

	if config.AppConfig.LocalRegistryCredentials.URL == "" {
		return nil, errors.New("custom registry URL is required when BringOwnRegistry is enabled")
	}

	if _, err := url.ParseRequestURI(string(config.AppConfig.LocalRegistryCredentials.URL)); err != nil {
		return nil, fmt.Errorf("invalid custom registry URL: %w", err)
	}

	if config.AppConfig.LocalRegistryCredentials.Username == "" || config.AppConfig.LocalRegistryCredentials.Password == "" {
		warnings = append(warnings, "username or password for custom registry is empty.")
	}

	if len(config.ZotConfigRaw) > 0 {
		warnings = append(warnings, "redundant zot_config provided for bring_own_registry: `true`.")
		config.ZotConfigRaw = json.RawMessage{}
	}

	return warnings, nil
}

// validateAndEnforceCronSchedules validates and defaults cron schedules.
func validateAndEnforceCronSchedules(config *Config) []string {
	var warnings []string

	if !isValidCronExpression(config.AppConfig.StateReplicationInterval) {
		config.AppConfig.StateReplicationInterval = DefaultFetchAndReplicateCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for state_replication_interval, using default schedule %s", DefaultFetchAndReplicateCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.RegisterSatelliteInterval) {
		config.AppConfig.RegisterSatelliteInterval = DefaultZTRCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for register_satellite_interval, using default schedule %s", DefaultZTRCronExpr))
	}

	if !isValidCronExpression(config.AppConfig.HeartbeatInterval) {
		config.AppConfig.HeartbeatInterval = DefaultHeartbeatCronExpr
		warnings = append(warnings, fmt.Sprintf("invalid schedule provided for heartbeat_interval, using default schedule %s", DefaultHeartbeatCronExpr))
	}

	return warnings
}

// validateAndEnforceZotConfig validates and defaults zot registry configuration.
func validateAndEnforceZotConfig(config *Config, bringOwnRegistry bool) ([]string, error) {
	var warnings []string

	if bringOwnRegistry {
		return warnings, nil
	}

	needsDefault := len(config.ZotConfigRaw) == 0 || strings.TrimSpace(string(config.ZotConfigRaw)) == "{}"
	if needsDefault {
		warnings = append(warnings, fmt.Sprintf(
			"empty zot_config provided. Defaulting to: %v", DefaultZotConfigJSON,
		))
		config.ZotConfigRaw = json.RawMessage(DefaultZotConfigJSON)
	}

	var zotConfig registry.ZotConfig
	if err := json.Unmarshal(config.ZotConfigRaw, &zotConfig); err != nil {
		return nil, fmt.Errorf("invalid zot_config: %w", err)
	}

	if zotConfig.HTTP.Address == "" || zotConfig.HTTP.Port == "" {
		warnings = append(warnings, "zot_config missing required http address/port. Applying defaults.")
		config.ZotConfigRaw = json.RawMessage(DefaultZotConfigJSON)
		if err := json.Unmarshal(config.ZotConfigRaw, &zotConfig); err != nil {
			return nil, fmt.Errorf("invalid default zot_config: %w", err)
		}
	}

	if config.AppConfig.LocalRegistryCredentials.URL == "" {
		warnings = append(warnings, fmt.Sprintf(
			"remote registry URL is empty. Defaulting to value from zot_config %s",
			DefaultRemoteRegistryURL,
		))
		config.AppConfig.LocalRegistryCredentials.URL = URL(zotConfig.GetRegistryURL())
	}

	return warnings, nil
}

// validateRegistryFallbackConfig validates registry fallback settings when enabled.
// An invalid registry name is a hard error: the name is used as a path element
// under /etc/containerd/certs.d by a code path that runs as root, so accepting a
// malformed one and carrying on would let remotely supplied configuration steer
// privileged filesystem writes.
func validateRegistryFallbackConfig(config *Config) ([]string, error) {
	validRuntimes := map[string]bool{
		"docker":     true,
		"containerd": true,
		"crio":       true,
		"podman":     true,
	}

	var warnings []string
	fb := config.AppConfig.RegistryFallback
	if !fb.Enabled {
		return warnings, nil
	}

	if len(fb.Registries) == 0 {
		warnings = append(warnings, "registry_fallback is enabled but no registries specified, defaulting to docker.io")
		config.AppConfig.RegistryFallback.Registries = []string{"docker.io"}
	}

	for _, r := range config.AppConfig.RegistryFallback.Registries {
		if err := ValidateRegistryName(r); err != nil {
			return warnings, fmt.Errorf("invalid registry_fallback entry: %w", err)
		}
	}

	for _, rt := range config.AppConfig.RegistryFallback.Runtimes {
		if !validRuntimes[rt] {
			warnings = append(warnings, fmt.Sprintf(
				"registry_fallback contains unknown runtime %q, valid values: docker, containerd, crio, podman", rt,
			))
		}
	}

	return warnings, nil
}

// ValidateRegistryName reports an error unless name is a bare registry host with
// an optional port, such as "docker.io", "localhost:5000" or "[::1]:5000".
//
// The name is used verbatim as a directory name under /etc/containerd/certs.d
// (see writeContainerdHostToml), so anything containing a path separator or a
// ".." segment would escape that directory and redirect root-owned MkdirAll and
// file writes elsewhere on the host. A registry host is the only meaningful
// value here and no valid host contains a separator, so rejecting them costs
// nothing and closes the traversal.
//
// A scheme prefix such as "https://" is rejected rather than stripped: it never
// produced a usable certs.d directory, so failing loudly beats silently writing
// a config containerd will not read.
func ValidateRegistryName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("registry entry is empty")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("registry entry %q has leading or trailing whitespace", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("registry entry %q must be a bare host[:port] with no scheme or path separator", name)
	}

	host, port, hasPort := splitRegistryPort(name)
	if hasPort {
		if err := validateRegistryPort(name, port); err != nil {
			return err
		}
	}

	return validateRegistryHost(name, host)
}

// splitRegistryPort separates an optional ":port" suffix, accounting for
// bracketed IPv6 literals whose address part contains colons of its own.
func splitRegistryPort(name string) (host, port string, hasPort bool) {
	if strings.HasPrefix(name, "[") {
		end := strings.LastIndex(name, "]")
		if end < 0 {
			return name, "", false
		}
		if rest := name[end+1:]; strings.HasPrefix(rest, ":") {
			return name[:end+1], rest[1:], true
		}
		return name, "", false
	}

	i := strings.LastIndex(name, ":")
	if i < 0 {
		return name, "", false
	}
	return name[:i], name[i+1:], true
}

func validateRegistryPort(name, port string) error {
	// strconv.Atoi accepts a leading '+' or '-' (e.g. "+443"), which is not a
	// valid port. Require every character to be a digit before parsing.
	if port == "" {
		return fmt.Errorf("registry entry %q has an invalid port %q", name, port)
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return fmt.Errorf("registry entry %q has an invalid port %q", name, port)
		}
	}

	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("registry entry %q has an invalid port %q", name, port)
	}
	return nil
}

func validateRegistryHost(name, host string) error {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		// Brackets are IPv6-only syntax. Accepting "[127.0.0.1]" would produce
		// an invalid "https://[127.0.0.1]:5000" server URL downstream, so an
		// address that parses as IPv4 is rejected here.
		ip := net.ParseIP(host[1 : len(host)-1])
		if ip == nil || ip.To4() != nil {
			return fmt.Errorf("registry entry %q is not a valid IPv6 literal", name)
		}
		return nil
	}

	if len(host) > 253 {
		return fmt.Errorf("registry entry %q exceeds the maximum host length of 253 characters", name)
	}

	// Splitting on "." also rejects "." and "..", which produce empty labels.
	for _, label := range strings.Split(host, ".") {
		if err := validateHostLabel(name, label); err != nil {
			return err
		}
	}

	return nil
}

func validateHostLabel(name, label string) error {
	if label == "" || len(label) > 63 {
		return fmt.Errorf("registry entry %q has an invalid host label %q", name, label)
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return fmt.Errorf("registry entry %q has a host label %q with a leading or trailing hyphen", name, label)
	}
	for _, c := range label {
		isAlphanumeric := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphanumeric && c != '-' {
			return fmt.Errorf("registry entry %q has a host label %q with an invalid character %q", name, label, c)
		}
	}
	return nil
}

// validateTLSConfig validates TLS configuration.
func validateTLSConfig(tls *TLSConfig) ([]string, error) {
	var warnings []string

	if tls.CertFile == "" && tls.KeyFile == "" && tls.CAFile == "" {
		return warnings, nil
	}

	if (tls.CertFile != "" && tls.KeyFile == "") || (tls.CertFile == "" && tls.KeyFile != "") {
		return warnings, errors.New("both cert_file and key_file must be provided together")
	}

	if tls.CertFile != "" {
		if _, err := os.Stat(tls.CertFile); os.IsNotExist(err) {
			return warnings, fmt.Errorf("TLS cert_file not found: %s", tls.CertFile)
		}
	}

	if tls.KeyFile != "" {
		if _, err := os.Stat(tls.KeyFile); os.IsNotExist(err) {
			return warnings, fmt.Errorf("TLS key_file not found: %s", tls.KeyFile)
		}
	}

	if tls.CAFile != "" {
		if _, err := os.Stat(tls.CAFile); os.IsNotExist(err) {
			return warnings, fmt.Errorf("TLS ca_file not found: %s", tls.CAFile)
		}
	}

	if tls.SkipVerify {
		warnings = append(warnings, "TLS skip_verify is enabled, certificate verification will be skipped")
	}

	return warnings, nil
}
