package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigManagerModifiers(t *testing.T) {
	cfg := &Config{
		AppConfig:    AppConfig{},
		StateConfig:  StateConfig{},
		ZotConfigRaw: json.RawMessage(`{"storage": {}}`),
	}
	cm, err := NewConfigManager("", "", "", "", true, cfg)
	require.NoError(t, err)

	tests := []struct {
		name    string
		mutator func(*Config)
		check   func(*testing.T, *Config)
	}{
		{
			name:    "SetStateURL",
			mutator: SetStateURL("http://stateurl"),
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "http://stateurl", c.StateConfig.StateURL)
			},
		},
		{
			name:    "SetGroundControlURL",
			mutator: SetGroundControlURL("http://groundcontrol"),
			check: func(t *testing.T, c *Config) {
				require.Equal(t, URL("http://groundcontrol"), c.AppConfig.GroundControlURL)
			},
		},
		{
			name:    "SetLogLevel",
			mutator: SetLogLevel("debug"),
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "debug", c.AppConfig.LogLevel)
			},
		},
		{
			name:    "SetUseUnsecure",
			mutator: SetUseUnsecure(true),
			check: func(t *testing.T, c *Config) {
				require.True(t, c.AppConfig.UseUnsecure)
			},
		},
		{
			name:    "SetRegisterSatelliteInterval",
			mutator: SetRegisterSatelliteInterval("@every 10m"),
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "@every 10m", c.AppConfig.RegisterSatelliteInterval)
			},
		},
		{
			name:    "SetBringOwnRegistry",
			mutator: SetBringOwnRegistry(true),
			check: func(t *testing.T, c *Config) {
				require.True(t, c.AppConfig.BringOwnRegistry)
			},
		},
		{
			name:    "SetLocalRegistryUsername",
			mutator: SetLocalRegistryUsername("user123"),
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "user123", c.AppConfig.LocalRegistryCredentials.Username)
			},
		},
		{
			name:    "SetLocalRegistryPassword",
			mutator: SetLocalRegistryPassword("pass123"),
			check: func(t *testing.T, c *Config) {
				require.Equal(t, "pass123", c.AppConfig.LocalRegistryCredentials.Password)
			},
		},
		{
			name: "SetRegistryFallbackConfig",
			mutator: SetRegistryFallbackConfig(RegistryFallbackConfig{
				Enabled:    true,
				Registries: []string{"docker.io", "quay.io"},
				Runtimes:   []string{"containerd"},
			}),
			check: func(t *testing.T, c *Config) {
				require.True(t, c.AppConfig.RegistryFallback.Enabled)
				require.Equal(t, []string{"docker.io", "quay.io"}, c.AppConfig.RegistryFallback.Registries)
				require.Equal(t, []string{"containerd"}, c.AppConfig.RegistryFallback.Runtimes)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm.With(tt.mutator)
			tt.check(t, cm.config)
		})
	}
}

func TestGetRegistryFallbackConfig(t *testing.T) {
	cfg := &Config{
		AppConfig: AppConfig{
			RegistryFallback: RegistryFallbackConfig{
				Enabled:    true,
				Registries: []string{"docker.io"},
				Runtimes:   []string{"containerd", "docker"},
			},
		},
		ZotConfigRaw: json.RawMessage(`{}`),
	}

	cm, err := NewConfigManager("", "", "", "", true, cfg)
	require.NoError(t, err)

	got := cm.GetRegistryFallbackConfig()
	require.True(t, got.Enabled)
	require.Equal(t, []string{"docker.io"}, got.Registries)
	require.Equal(t, []string{"containerd", "docker"}, got.Runtimes)
}

func TestGetRegistryFallbackConfigEmpty(t *testing.T) {
	cfg := &Config{
		AppConfig:    AppConfig{},
		ZotConfigRaw: json.RawMessage(`{}`),
	}

	cm, err := NewConfigManager("", "", "", "", true, cfg)
	require.NoError(t, err)

	got := cm.GetRegistryFallbackConfig()
	require.False(t, got.Enabled)
	require.Nil(t, got.Registries)
	require.Nil(t, got.Runtimes)
}
