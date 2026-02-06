package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func newTestConfigManager(t *testing.T, cfg *config.Config) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	prevPath := filepath.Join(dir, "prev_config.json")
	cm, err := config.NewConfigManager(configPath, prevPath, "token", "http://gc:8080", false, cfg)
	require.NoError(t, err)
	return cm
}

func TestResolveLocalRegistryEndpoint_BYO(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "strips http prefix",
			url:      "http://registry:5000",
			expected: "registry:5000",
		},
		{
			name:     "strips https prefix",
			url:      "https://registry.example.com:5000",
			expected: "registry.example.com:5000",
		},
		{
			name:     "no prefix passthrough",
			url:      "registry:5000",
			expected: "registry:5000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				AppConfig: config.AppConfig{
					BringOwnRegistry: true,
					LocalRegistryCredentials: config.RegistryCredentials{
						URL: config.URL(tt.url),
					},
				},
				ZotConfigRaw: json.RawMessage(`{}`),
			}
			cm := newTestConfigManager(t, cfg)

			endpoint, err := resolveLocalRegistryEndpoint(cm)
			require.NoError(t, err)
			require.Equal(t, tt.expected, endpoint)
		})
	}
}

func TestResolveLocalRegistryEndpoint_Zot(t *testing.T) {
	tests := []struct {
		name        string
		zotJSON     string
		expected    string
		expectError bool
		errContains string
	}{
		{
			name:     "valid zot config",
			zotJSON:  `{"http":{"address":"0.0.0.0","port":"8585"}}`,
			expected: "0.0.0.0:8585",
		},
		{
			name:     "custom address and port",
			zotJSON:  `{"http":{"address":"127.0.0.1","port":"9090"}}`,
			expected: "127.0.0.1:9090",
		},
		{
			name:        "missing http section",
			zotJSON:     `{"storage":{}}`,
			expectError: true,
			errContains: "missing 'http' section",
		},
		{
			name:        "missing address",
			zotJSON:     `{"http":{"port":"8585"}}`,
			expectError: true,
			errContains: "missing 'address' or 'port'",
		},
		{
			name:        "missing port",
			zotJSON:     `{"http":{"address":"0.0.0.0"}}`,
			expectError: true,
			errContains: "missing 'address' or 'port'",
		},
		{
			name:        "invalid json",
			zotJSON:     `not-json`,
			expectError: true,
			errContains: "unmarshalling zot config",
		},
		{
			name:        "empty json object",
			zotJSON:     `{}`,
			expectError: true,
			errContains: "missing 'http' section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				AppConfig: config.AppConfig{
					BringOwnRegistry: false,
				},
				ZotConfigRaw: json.RawMessage(tt.zotJSON),
			}
			cm := newTestConfigManager(t, cfg)

			endpoint, err := resolveLocalRegistryEndpoint(cm)
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, endpoint)
		})
	}
}
