package registry

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// defaultSyncPollInterval is the default polling interval for checking
// upstream registry changes to already-cached images.
const defaultSyncPollInterval = "6h"

// SyncRegistryConfig represents a single upstream registry in the Zot sync extension.
type SyncRegistryConfig struct {
	URLs           []string            `json:"urls"`
	OnDemand       bool                `json:"onDemand"`
	PollInterval   string              `json:"pollInterval,omitempty"`
	TLSVerify      bool                `json:"tlsVerify"`
	MaxRetries     int                 `json:"maxRetries,omitempty"`
	RetryDelay     string              `json:"retryDelay,omitempty"`
	PreserveDigest bool                `json:"preserveDigest"`
	Content        []SyncContentConfig `json:"content"`
}

// SyncContentConfig specifies which content to sync from an upstream registry.
type SyncContentConfig struct {
	Prefix string `json:"prefix"`
}

// SyncExtensionConfig is the Zot extensions.sync configuration.
type SyncExtensionConfig struct {
	Enable          bool                 `json:"enable"`
	CredentialsFile string               `json:"credentialsFile"`
	Registries      []SyncRegistryConfig `json:"registries"`
}

// ProxyCacheParams holds the parameters needed to build a proxy-cache Zot config.
type ProxyCacheParams struct {
	BaseConfig      json.RawMessage
	UpstreamURL     string
	CredentialsFile string
	PollInterval    string
	UseUnsecure     bool
}

// BuildProxyCacheZotConfig augments a base Zot config with the sync extension
// for on-demand proxy-cache mode. It also enables Docker v2s2 compatibility
// so that digests are preserved.
func BuildProxyCacheZotConfig(params ProxyCacheParams) (json.RawMessage, error) {
	var zotCfg map[string]any
	if err := json.Unmarshal(params.BaseConfig, &zotCfg); err != nil {
		return nil, fmt.Errorf("unmarshal base Zot config: %w", err)
	}

	// Enable Docker v2 schema 2 compatibility for digest preservation
	httpSection, ok := zotCfg["http"].(map[string]any)
	if !ok {
		httpSection = map[string]any{}
	}
	httpSection["compat"] = []string{"docker2s2"}
	zotCfg["http"] = httpSection

	// Build sync extension
	pollInterval := params.PollInterval
	if pollInterval == "" {
		pollInterval = defaultSyncPollInterval
	}

	syncCfg := SyncExtensionConfig{
		Enable:          true,
		CredentialsFile: params.CredentialsFile,
		Registries: []SyncRegistryConfig{
			{
				URLs:           []string{params.UpstreamURL},
				OnDemand:       true,
				PollInterval:   pollInterval,
				TLSVerify:      !params.UseUnsecure,
				MaxRetries:     3,
				RetryDelay:     "5m",
				PreserveDigest: true,
				Content: []SyncContentConfig{
					{Prefix: "**"},
				},
			},
		},
	}

	extensions, ok := zotCfg["extensions"].(map[string]any)
	if !ok {
		extensions = map[string]any{}
	}
	extensions["sync"] = syncCfg
	zotCfg["extensions"] = extensions

	result, err := json.MarshalIndent(zotCfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal proxy-cache Zot config: %w", err)
	}
	return result, nil
}

// SyncCredentials represents the credentials file format expected by Zot's
// sync extension (credentialsFile).
type SyncCredentials map[string]SyncCredentialEntry

// SyncCredentialEntry holds username/password for a single registry.
type SyncCredentialEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// WriteSyncCredentialsFile writes a Zot sync credentials file to disk.
// The file maps registry host:port to username/password pairs.
func WriteSyncCredentialsFile(filePath, registryURL, username, password string) error {
	host, err := extractHost(registryURL)
	if err != nil {
		return fmt.Errorf("extract host from registry URL %q: %w", registryURL, err)
	}

	creds := SyncCredentials{
		host: {
			Username: username,
			Password: password,
		},
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sync credentials: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("write sync credentials file: %w", err)
	}

	return nil
}

// extractHost returns the host (with port if present) from a URL string.
func extractHost(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("no host in URL %q", rawURL)
	}
	return parsed.Host, nil
}
