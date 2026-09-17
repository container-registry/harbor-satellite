package main

import (
	"path/filepath"
	"testing"

	runtime "github.com/container-registry/harbor-satellite/internal/satellite/container_runtime"
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
			}
			cm := newTestConfigManager(t, cfg)

			endpoint := resolveLocalRegistryEndpoint(cm)
			require.Equal(t, tt.expected, endpoint)
		})
	}
}

func TestResolveLocalRegistryEndpoint_LocalStore(t *testing.T) {
	cm := newTestConfigManager(t, &config.Config{})
	endpoint := resolveLocalRegistryEndpoint(cm)
	require.Empty(t, endpoint)
}

func TestResolveCRIAndApply(t *testing.T) {
	t.Run("local OCI store does not configure registry fallback", func(t *testing.T) {
		cfg := &config.Config{
			AppConfig: config.AppConfig{
				RegistryFallback: config.RegistryFallbackConfig{Enabled: true},
			},
		}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, false, "")
		require.Nil(t, results)
	})

	t.Run("noFallback returns nil", func(t *testing.T) {
		cfg := &config.Config{}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, true, "localhost:8585")
		require.Nil(t, results)
	})

	t.Run("no config no mirrors returns nil", func(t *testing.T) {
		cfg := &config.Config{}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, false, "localhost:8585")
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
		}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"containerd:quay.io"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:8585")
		require.Len(t, results, 1)
		require.Equal(t, runtime.CRIType("unsupported_cri"), results[0].CRI)
		require.False(t, results[0].Success)
	})

	t.Run("mirrors used when config disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"badformat"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:8585")
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
