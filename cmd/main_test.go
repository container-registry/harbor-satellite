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
		{
			name:     "localhost address",
			zotJSON:  `{"http":{"address":"localhost","port":"5000"}}`,
			expected: "localhost:5000",
		},
		{
			name:     "non-standard port",
			zotJSON:  `{"http":{"address":"0.0.0.0","port":"12345"}}`,
			expected: "0.0.0.0:12345",
		},
		{
			name:        "http section with wrong type",
			zotJSON:     `{"http":"wrong_type"}`,
			expectError: true,
			errContains: "missing 'http' section",
		},
		{
			name:        "address with wrong type",
			zotJSON:     `{"http":{"address":123,"port":"8585"}}`,
			expectError: true,
			errContains: "missing 'address' or 'port'",
		},
		{
			name:        "port with wrong type",
			zotJSON:     `{"http":{"address":"0.0.0.0","port":8585}}`,
			expectError: true,
			errContains: "missing 'address' or 'port'",
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

func TestMirrorFlags(t *testing.T) {
	t.Run("String method", func(t *testing.T) {
		flags := mirrorFlags{"cri1:registry1", "cri2:registry2"}
		result := flags.String()
		require.NotEmpty(t, result)
		require.Contains(t, result, "cri1:registry1")
		require.Contains(t, result, "cri2:registry2")
	})

	t.Run("Set method", func(t *testing.T) {
		var flags mirrorFlags
		err := flags.Set("cri1:registry1")
		require.NoError(t, err)
		require.Len(t, flags, 1)
		require.Equal(t, "cri1:registry1", flags[0])

		err = flags.Set("cri2:registry2")
		require.NoError(t, err)
		require.Len(t, flags, 2)
	})

	t.Run("Set multiple values", func(t *testing.T) {
		var flags mirrorFlags
		values := []string{"cri1:reg1", "cri2:reg2,reg3", "cri3:reg4"}
		for _, v := range values {
			err := flags.Set(v)
			require.NoError(t, err)
		}
		require.Len(t, flags, 3)
	})
}

func TestSatelliteOptions(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		opts := SatelliteOptions{}
		require.False(t, opts.JSONLogging)
		require.Empty(t, opts.GroundControlURL)
		require.Empty(t, opts.Token)
		require.False(t, opts.UseUnsecure)
		require.False(t, opts.SPIFFEEnabled)
		require.Empty(t, opts.Mirrors)
	})

	t.Run("with values", func(t *testing.T) {
		opts := SatelliteOptions{
			JSONLogging:      true,
			GroundControlURL: "http://gc:8080",
			Token:            "test-token",
			UseUnsecure:      true,
			SPIFFEEnabled:    true,
		}
		require.True(t, opts.JSONLogging)
		require.Equal(t, "http://gc:8080", opts.GroundControlURL)
		require.Equal(t, "test-token", opts.Token)
		require.True(t, opts.UseUnsecure)
		require.True(t, opts.SPIFFEEnabled)
	})
}

func TestResolveLocalRegistryEndpoint_BYO_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "trailing slash preserved",
			url:      "http://registry:5000/",
			expected: "registry:5000/",
		},
		{
			name:     "multiple slashes preserved",
			url:      "http://registry:5000///",
			expected: "registry:5000///",
		},
		{
			name:     "with path",
			url:      "http://registry:5000/v2",
			expected: "registry:5000/v2",
		},
		{
			name:     "just hostname no port",
			url:      "http://registry",
			expected: "registry",
		},
		{
			name:     "https with port",
			url:      "https://secure.registry.com:443",
			expected: "secure.registry.com:443",
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