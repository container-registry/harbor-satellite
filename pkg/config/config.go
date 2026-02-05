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

type AppConfig struct {
	GroundControlURL          URL                 `json:"ground_control_url,omitempty"`
	LogLevel                  string              `json:"log_level,omitempty"`
	UseUnsecure               bool                `json:"use_unsecure,omitempty"`
	StateReplicationInterval  string              `json:"state_replication_interval,omitempty"`
	RegisterSatelliteInterval string              `json:"register_satellite_interval,omitempty"`
	HeartbeatInterval         string              `json:"heartbeat_interval,omitempty"`
	Metrics                   MetricsConfig       `json:"metrics,omitempty"`
	BringOwnRegistry          bool                `json:"bring_own_registry,omitempty"`
	LocalRegistryCredentials  RegistryCredentials `json:"local_registry,omitempty"`
	TLS                       TLSConfig           `json:"tls,omitempty"`
	SPIFFE                    SPIFFEConfig        `json:"spiffe,omitempty"`
	EncryptConfig             bool                `json:"encrypt_config,omitempty"`
	AuditLogPath              string              `json:"audit_log_path,omitempty"`
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
