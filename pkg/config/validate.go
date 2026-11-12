package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
	"github.com/rs/zerolog"
)

// ValidateAndEnforceDefaults validates and normalizes the given config.
// It applies default values where required, verifies URLs, @every intervals,
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

	more, err := validateApp(config)
	warnings = append(warnings, more...)
	if err != nil {
		return nil, warnings, err
	}
	return config, warnings, nil
}

// validateApp runs the app_config checks that need neither the Ground
// Control URL nor the environment.
func validateApp(config *Config) ([]string, error) {
	var warnings []string
	if config.AppConfig.BringOwnRegistry {
		borWarnings, borErr := validateBringOwnRegistry(config)
		warnings = append(warnings, borWarnings...)
		if borErr != nil {
			return warnings, borErr
		}
	}

	warnings = append(warnings, validateAndEnforceCronSchedules(config)...)

	tlsWarnings, tlsErr := validateTLSConfig(&config.AppConfig.TLS)
	warnings = append(warnings, tlsWarnings...)
	if tlsErr != nil {
		return warnings, tlsErr
	}

	warnings = append(warnings, validateRegistryFallbackConfig(config)...)

	if err := validateAndEnforcePeerDistribution(config); err != nil {
		return warnings, err
	}

	warnings = append(warnings, validateAndEnforceAuditConfig(config)...)

	networkWarnings, networkErr := validateAndEnforceNetwork(config)
	warnings = append(warnings, networkWarnings...)
	if networkErr != nil {
		return warnings, networkErr
	}

	switch config.AppConfig.RegistryAuth.Mode {
	case "", RegistryAuthRobot:
	case RegistryAuthWorkloadIdentity:
		return warnings, errors.New("app_config.registry_auth.mode workload_identity needs Harbor 3 workload identity federation, which this release cannot use yet; use robot")
	default:
		return warnings, fmt.Errorf("app_config.registry_auth.mode %q: want robot or workload_identity", config.AppConfig.RegistryAuth.Mode)
	}
	return warnings, nil
}

// ValidateAppConfig is ValidateAndEnforceDefaults in Ground Control's mode
// (plan item 7.4): no ground_control_url needed (GC knows its own URL), no
// environment read (GC's USE_UNSECURE is not the satellite's). Where the
// satellite replaces an invalid interval with its default, it rejects
// one. It also rejects an admission mode policybundle.ParseMode rejects and a bundle of
// another model version. It returns the warnings and leaves app unchanged.
func ValidateAppConfig(app AppConfig) (warnings []string, err error) {
	raw, err := json.Marshal(app)
	if err != nil {
		return nil, fmt.Errorf("encode app_config: %w", err)
	}
	cfg := &Config{}
	if err = json.Unmarshal(raw, &cfg.AppConfig); err != nil {
		return nil, fmt.Errorf("copy app_config: %w", err)
	}
	warnings = validateAndEnforceLogLevel(cfg)
	if err = rejectInvalidSchedules(&cfg.AppConfig); err != nil {
		return warnings, err
	}
	more, err := validateApp(cfg)
	warnings = append(warnings, more...)
	if err != nil {
		return warnings, err
	}
	if _, err := policybundle.ParseMode(cfg.AppConfig.Admission.Mode); err != nil {
		return warnings, fmt.Errorf("app_config.admission.mode: %w", err)
	}
	if b := cfg.AppConfig.Admission.Bundle; b != nil && b.ModelVersion != policybundle.ModelVersion {
		return warnings, fmt.Errorf("app_config.admission.bundle: model version %q, want %q", b.ModelVersion, policybundle.ModelVersion)
	}
	return warnings, nil
}

// validateAndEnforceNetwork checks the optional transfer quota. A disabled
// block keeps a malformed limit from rejecting the rest of the config. An
// enabled quota with an empty window defaults to day.
func validateAndEnforceNetwork(config *Config) ([]string, error) {
	network := &config.AppConfig.Network
	if !network.Quota.Enabled {
		return nil, nil
	}
	if network.Quota.Limit == "" {
		return nil, errors.New("network.quota.limit is required when quota is enabled")
	}
	if _, err := ParseByteSize(network.Quota.Limit); err != nil {
		return nil, fmt.Errorf("network.quota.limit: %w", err)
	}

	var warnings []string
	switch strings.ToLower(strings.TrimSpace(network.Quota.Window)) {
	case "":
		network.Quota.Window = "day"
		warnings = append(warnings, "network.quota.window defaulted to day")
	case "sync", "day", "week":
		network.Quota.Window = strings.ToLower(strings.TrimSpace(network.Quota.Window))
	default:
		return nil, fmt.Errorf("network.quota.window %q must be sync, day, or week", network.Quota.Window)
	}
	return warnings, nil
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

// isValidSchedule reports whether expr is "@every <positive duration>", the
// only form the satellite scheduler runs. A standard cron line such as
// "*/5 * * * *" would pass a cron parser but stop the satellite at start.
func isValidSchedule(expr string) bool {
	d, ok := strings.CutPrefix(expr, "@every ")
	if !ok {
		return false
	}
	parsed, err := time.ParseDuration(d)
	return err == nil && parsed > 0
}

// scheduleFields are the app_config intervals and their defaults.
func scheduleFields(app *AppConfig) []scheduleField {
	return []scheduleField{
		{"state_replication_interval", DefaultFetchAndReplicateCronExpr, &app.StateReplicationInterval},
		{"register_satellite_interval", DefaultZTRCronExpr, &app.RegisterSatelliteInterval},
		{"heartbeat_interval", DefaultHeartbeatCronExpr, &app.HeartbeatInterval},
	}
}

type scheduleField struct {
	field, def string
	value      *string
}

// rejectInvalidSchedules is Ground Control's check: an interval that is set
// must be "@every <positive duration>", so a config the satellites would
// silently replace with the default is never stored.
func rejectInvalidSchedules(app *AppConfig) error {
	var errs []error
	for _, s := range scheduleFields(app) {
		if *s.value != "" && !isValidSchedule(*s.value) {
			errs = append(errs, fmt.Errorf("app_config.%s %q: want \"@every <positive duration>\", such as %q", s.field, *s.value, s.def))
		}
	}
	return errors.Join(errs...)
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

	return warnings, nil
}

// validateAndEnforceCronSchedules replaces an interval that is not
// "@every <positive duration>" with its default and a warning.
func validateAndEnforceCronSchedules(config *Config) []string {
	var warnings []string
	for _, s := range scheduleFields(&config.AppConfig) {
		if w := enforceSchedule(s.field, s.value, s.def); w != "" {
			warnings = append(warnings, w)
		}
	}
	return warnings
}

// enforceSchedule sets an absent schedule to def without a warning, since
// leaving it out is how a config asks for the default; an invalid one is
// replaced with a warning that quotes it.
func enforceSchedule(field string, value *string, def string) string {
	if *value == "" {
		*value = def
		return ""
	}
	if isValidSchedule(*value) {
		return ""
	}
	invalid := *value
	*value = def
	return fmt.Sprintf("invalid schedule %q provided for %s, using default schedule %s", invalid, field, def)
}

// validateAndEnforcePeerDistribution defaults and checks the opt-in peer block.
// An omitted block is left zero so existing configs do not grow a new section.
func validateAndEnforcePeerDistribution(config *Config) error {
	p := &config.AppConfig.PeerDistribution
	if p.IsZero() {
		return nil
	}

	if err := enforcePeerDistributionDefaults(p); err != nil {
		return err
	}

	if err := validatePeerList(p.StaticPeers, config.AppConfig.UseUnsecure, "static_peers"); err != nil {
		return err
	}

	return validatePeerList(p.GCPeers, config.AppConfig.UseUnsecure, "gc_peers")
}

func enforcePeerDistributionDefaults(p *PeerDistributionConfig) error {
	switch p.ReachoutSats {
	case "":
		p.ReachoutSats = DefaultPeerReachoutSats
	case "group", "global":
	default:
		return fmt.Errorf("peer_distribution.reachout_sats %q is invalid (expected group|global)", p.ReachoutSats)
	}

	if p.Timeout == "" {
		p.Timeout = DefaultPeerTimeout
	} else if d, err := time.ParseDuration(p.Timeout); err != nil || d <= 0 {
		return fmt.Errorf("peer_distribution.timeout %q must be a duration greater than 0", p.Timeout)
	}

	if p.Retries == nil {
		v := DefaultPeerRetries
		p.Retries = &v
	} else if *p.Retries < 0 {
		return fmt.Errorf("peer_distribution.retries must be >= 0")
	}

	if p.Concurrency < 0 {
		return fmt.Errorf("peer_distribution.concurrency must be >= 1")
	}
	if p.Concurrency == 0 {
		p.Concurrency = DefaultPeerConcurrency
	}

	return nil
}

func validatePeerList(peers []PeerDescriptor, useUnsecure bool, field string) error {
	for i, peer := range peers {
		if err := validatePeerDescriptor(peer, useUnsecure, field, i); err != nil {
			return err
		}
	}

	return nil
}

func validatePeerDescriptor(peer PeerDescriptor, useUnsecure bool, field string, index int) error {
	if strings.TrimSpace(peer.ID) == "" {
		return fmt.Errorf("peer_distribution.%s[%d].id is required", field, index)
	}
	if strings.TrimSpace(string(peer.URL)) == "" {
		return fmt.Errorf("peer_distribution.%s[%d].url is required", field, index)
	}

	parsed, err := url.ParseRequestURI(string(peer.URL))
	if err != nil {
		return fmt.Errorf("peer_distribution.%s[%d].url is invalid: %w", field, index, err)
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("peer_distribution.%s[%d].url must include a host", field, index)
	}
	if parsed.User != nil {
		return fmt.Errorf("peer_distribution.%s[%d].url must not include userinfo; use username and password fields for credentials", field, index)
	}

	if _, err := validateTLSConfig(&peer.TLS); err != nil {
		return fmt.Errorf("peer_distribution.%s[%d].tls: %w", field, index, err)
	}

	return validatePeerTransport(parsed.Scheme, peerHasCredentials(peer), peer.TLS.SkipVerify, useUnsecure, field, index)
}

func peerHasCredentials(peer PeerDescriptor) bool {
	return peer.Username != "" || peer.Password != ""
}

// ApplyRetainedPeerTransport keeps the local peer block when the fetched config
// omitted it. When a kept or backfilled peer uses HTTP and this process is
// already insecure, use_unsecure is set so validation accepts that peer. A
// fetched peer block keeps its own fields, and an empty static_peers list is
// filled from the local operator allow-list. A fetched use_unsecure of true
// stays true.
func ApplyRetainedPeerTransport(remote *Config, local PeerDistributionConfig, processUseUnsecure bool) {
	if remote == nil {
		return
	}
	omitted := remote.AppConfig.PeerDistribution.IsZero()
	hadStaticPeers := len(remote.AppConfig.PeerDistribution.StaticPeers) > 0
	PreservePeerDistribution(&remote.AppConfig.PeerDistribution, &local)
	if !processUseUnsecure || remote.AppConfig.UseUnsecure {
		return
	}
	keptHTTP := omitted && peerListUsesHTTP(local)
	backfilledHTTP := !omitted && !hadStaticPeers && descriptorsUseHTTP(remote.AppConfig.PeerDistribution.StaticPeers)
	if keptHTTP || backfilledHTTP {
		remote.AppConfig.UseUnsecure = true
	}
}

func peerListUsesHTTP(peers PeerDistributionConfig) bool {
	return descriptorsUseHTTP(peers.StaticPeers) || descriptorsUseHTTP(peers.GCPeers)
}

func descriptorsUseHTTP(peers []PeerDescriptor) bool {
	for _, peer := range peers {
		parsed, err := url.Parse(string(peer.URL))
		if err != nil {
			continue
		}
		if parsed.Scheme == "http" {
			return true
		}
	}
	return false
}

func validatePeerTransport(scheme string, hasCreds, skipVerify, useUnsecure bool, field string, index int) error {
	switch scheme {
	case "http":
		if !useUnsecure {
			return fmt.Errorf("peer_distribution.%s[%d] uses HTTP but use_unsecure is false", field, index)
		}
		if hasCreds {
			return fmt.Errorf("peer_distribution.%s[%d] must not send credentials over HTTP", field, index)
		}
	case "https":
		if hasCreds && skipVerify {
			return fmt.Errorf("peer_distribution.%s[%d] credentials require HTTPS with skip_verify=false", field, index)
		}
	default:
		return fmt.Errorf("peer_distribution.%s[%d].url scheme %q is invalid (expected http|https)", field, index, scheme)
	}

	return nil
}

// validateRegistryFallbackConfig validates registry fallback settings when enabled.
func validateRegistryFallbackConfig(config *Config) []string {
	validRuntimes := map[string]bool{
		"docker":     true,
		"containerd": true,
		"crio":       true,
		"podman":     true,
	}

	var warnings []string
	fb := config.AppConfig.RegistryFallback
	if !fb.Enabled {
		return warnings
	}

	if len(fb.Registries) == 0 {
		warnings = append(warnings, "registry_fallback is enabled but no registries specified, defaulting to docker.io")
		config.AppConfig.RegistryFallback.Registries = []string{"docker.io"}
	}

	for _, r := range config.AppConfig.RegistryFallback.Registries {
		if strings.TrimSpace(r) == "" {
			warnings = append(warnings, "registry_fallback contains an empty registry entry")
		}
	}

	for _, rt := range config.AppConfig.RegistryFallback.Runtimes {
		if !validRuntimes[rt] {
			warnings = append(warnings, fmt.Sprintf(
				"registry_fallback contains unknown runtime %q, valid values: docker, containerd, crio, podman", rt,
			))
		}
	}

	return warnings
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
