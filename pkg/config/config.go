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

// DirectDeliveryConfig holds settings for writing image tarballs directly
// to a Kubernetes node's image directory (e.g. k3s/RKE2 agent images dir).
// This is an experimental feature that enables satellite to deliver images
// without requiring pods to pull from a registry.
type DirectDeliveryConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	ImageDir string `json:"image_dir,omitempty"` // auto-detected if empty
}

// SignaturePolicy controls whether and how the satellite verifies cosign
// signatures before replicating an image to the local registry.
//
// When enabled, the satellite fetches the cosign signature OCI artifact
// (stored at <repo>:sha256-<digest>.sig) and verifies at least one layer
// signature against the provided ECDSA public keys.
//
// Action "block" (default) aborts replication on failure.
// Action "warn" logs a warning but continues replication.
type SignaturePolicy struct {
	Enabled    bool     `json:"enabled,omitempty"`
	PublicKeys []string `json:"public_keys,omitempty"`
	Action     string   `json:"action,omitempty"`
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
	SignaturePolicy           SignaturePolicy         `json:"signature_policy,omitempty"`
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
