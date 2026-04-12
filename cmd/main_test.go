package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	runtime "github.com/container-registry/harbor-satellite/internal/container_runtime"
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
			zotJSON:  `{"http":{"address":"0.0.0.0","port":"5000"}}`,
			expected: "0.0.0.0:5000",
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
			zotJSON:     `{"http":{"port":"5000"}}`,
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

func TestResolveCRIAndApply(t *testing.T) {
	t.Run("noFallback returns nil", func(t *testing.T) {
		cfg := &config.Config{
			ZotConfigRaw: json.RawMessage(`{}`),
		}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, true, "localhost:5000")
		require.Nil(t, results)
	})

	t.Run("no config no mirrors returns nil", func(t *testing.T) {
		cfg := &config.Config{
			ZotConfigRaw: json.RawMessage(`{}`),
		}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, false, "localhost:5000")
		require.Nil(t, results)
	})

	t.Run("config file wins over mirrors", func(t *testing.T) {
		cfg := &config.Config{
			AppConfig: config.AppConfig{
				RegistryFallback: config.RegistryFallbackConfig{
					Enabled:    true,
					Registries: []string{"docker.io"},
					Runtimes:   []string{"unsupported_cri"},
				},
			},
			ZotConfigRaw: json.RawMessage(`{}`),
		}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"containerd:quay.io"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:5000")
		require.Len(t, results, 1)
		require.Equal(t, runtime.CRIType("unsupported_cri"), results[0].CRI)
		require.False(t, results[0].Success)
	})

	t.Run("mirrors used when config disabled", func(t *testing.T) {
		cfg := &config.Config{
			ZotConfigRaw: json.RawMessage(`{}`),
		}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"badformat"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:5000")
		require.Nil(t, results)
	})
}

func TestMirrorFlags(t *testing.T) {
	t.Run("Set accumulates values", func(t *testing.T) {
		var m mirrorFlags
		require.NoError(t, m.Set("containerd:docker.io"))
		require.NoError(t, m.Set("docker:true"))
		require.Len(t, m, 2)
		require.Equal(t, "containerd:docker.io", m[0])
		require.Equal(t, "docker:true", m[1])
	})

	t.Run("String returns formatted output", func(t *testing.T) {
		m := mirrorFlags{"containerd:docker.io", "docker:true"}
		require.Equal(t, "[containerd:docker.io docker:true]", m.String())
	})

	t.Run("empty String", func(t *testing.T) {
		var m mirrorFlags
		require.Equal(t, "[]", m.String())
	})
}
