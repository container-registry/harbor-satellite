package config

import (
	"encoding/json"

	"github.com/rs/zerolog"
)

type URL string

type RegistryCredentials struct {
	URL      `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// TLSConfig holds TLS settings for secure connections.
type TLSConfig struct {
	CertFile   string `json:"cert_file,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
	CAFile     string `json:"ca_file,omitempty"`
	SkipVerify bool   `json:"skip_verify,omitempty"`
}

// SPIFFEConfig holds SPIFFE/SPIRE authentication settings.
type SPIFFEConfig struct {
	Enabled          bool   `json:"enabled,omitempty"`
	EndpointSocket   string `json:"endpoint_socket,omitempty"`
	ExpectedServerID string `json:"expected_server_id,omitempty"`
}

type MetricsConfig struct {
	CollectCPU     bool `json:"collect_cpu,omitempty"`
	CollectMemory  bool `json:"collect_memory,omitempty"`
	CollectStorage bool `json:"collect_storage,omitempty"`
}

type RegistryFallbackConfig struct {
	Enabled    bool     `json:"enabled,omitempty"`
	Registries []string `json:"registries,omitempty"`
	Runtimes   []string `json:"runtimes,omitempty"`
}

// AuditConfig controls the security-event audit log destination and rotation policy.
// When Enabled is false (default), audit events are discarded.
type AuditConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	FilePath string `json:"file_path,omitempty"`
	// MaxSizeMB, MaxBackups and MaxAgeDays are pointers so an omitted field
	// (nil) can be told apart from an explicit 0. An omitted field means "use
	// the default", matching Ground Control (whose env vars default when
	// unset). An explicit 0 on MaxBackups/MaxAgeDays is a deliberate "retain
	// everything" per lumberjack semantics and is preserved.
	MaxSizeMB  *int `json:"max_size_mb,omitempty"`
	MaxBackups *int `json:"max_backups,omitempty"`
	MaxAgeDays *int `json:"max_age_days,omitempty"`
}

// MaxSizeMBOrDefault returns the configured rotation size, or the default when unset.
func (a AuditConfig) MaxSizeMBOrDefault() int {
	if a.MaxSizeMB == nil {
		return DefaultAuditMaxSizeMB
	}
	return *a.MaxSizeMB
}

// MaxBackupsOrDefault returns the configured backup count, or the default when unset.
func (a AuditConfig) MaxBackupsOrDefault() int {
	if a.MaxBackups == nil {
		return DefaultAuditMaxBackups
	}
	return *a.MaxBackups
}

// MaxAgeDaysOrDefault returns the configured retention age, or the default when unset.
func (a AuditConfig) MaxAgeDaysOrDefault() int {
	if a.MaxAgeDays == nil {
		return DefaultAuditMaxAgeDays
	}
	return *a.MaxAgeDays
}

// Equal reports whether two audit configs resolve to the same effective
// settings. It compares effective values, not pointer identity, so a hot
// reload that yields fresh pointers with unchanged values is not mistaken
// for a change.
func (a AuditConfig) Equal(b AuditConfig) bool {
	return a.Enabled == b.Enabled &&
		a.FilePath == b.FilePath &&
		a.MaxSizeMBOrDefault() == b.MaxSizeMBOrDefault() &&
		a.MaxBackupsOrDefault() == b.MaxBackupsOrDefault() &&
		a.MaxAgeDaysOrDefault() == b.MaxAgeDaysOrDefault()
}

// DirectDeliveryConfig holds settings for writing image tarballs directly
// to a Kubernetes node's image directory (e.g. k3s/RKE2 agent images dir).
// This is an experimental feature that enables satellite to deliver images
// without requiring pods to pull from a registry.
type DirectDeliveryConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	ImageDir string `json:"image_dir,omitempty"` // auto-detected if empty
}

type AppConfig struct {
	GroundControlURL          URL                    `json:"ground_control_url,omitempty"`
	LogLevel                  string                 `json:"log_level,omitempty"`
	UseUnsecure               bool                   `json:"use_unsecure,omitempty"`
	StateReplicationInterval  string                 `json:"state_replication_interval,omitempty"`
	RegisterSatelliteInterval string                 `json:"register_satellite_interval,omitempty"`
	HeartbeatInterval         string                 `json:"heartbeat_interval,omitempty"`
	Metrics                   MetricsConfig          `json:"metrics,omitempty"`
	BringOwnRegistry          bool                   `json:"bring_own_registry,omitempty"`
	LocalRegistryCredentials  RegistryCredentials    `json:"local_registry,omitempty"`
	TLS                       TLSConfig              `json:"tls,omitempty"`
	SPIFFE                    SPIFFEConfig           `json:"spiffe,omitempty"`
	EncryptConfig             bool                   `json:"encrypt_config,omitempty"`
	RegistryFallback          RegistryFallbackConfig `json:"registry_fallback,omitempty"`
	HarborRegistryURL         string                 `json:"harbor_registry_url,omitempty"`
	DirectDelivery            DirectDeliveryConfig   `json:"direct_delivery,omitempty"`
	Audit                     AuditConfig            `json:"audit,omitempty"`
}

type StateConfig struct {
	RegistryCredentials RegistryCredentials `json:"auth,omitempty"`
	StateURL            string              `json:"state,omitempty"`
}

type Config struct {
	StateConfig  StateConfig     `json:"state_config,omitempty"`
	AppConfig    AppConfig       `json:"app_config,omitempty"`
	ZotConfigRaw json.RawMessage `json:"zot_config,omitempty"`
}

var validLogLevels = map[string]bool{
	zerolog.LevelDebugValue: true,
	zerolog.LevelInfoValue:  true,
	zerolog.LevelWarnValue:  true,
	zerolog.LevelErrorValue: true,
	zerolog.LevelFatalValue: true,
	zerolog.LevelPanicValue: true,
}
