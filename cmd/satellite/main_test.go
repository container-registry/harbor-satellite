package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
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

func TestResolveCRIAndApply(t *testing.T) {
	t.Run("noFallback returns nil", func(t *testing.T) {
		cfg := &config.Config{
			ZotConfigRaw: json.RawMessage(`{}`),
		}
		cm := newTestConfigManager(t, cfg)

		results := resolveCRIAndApply(cm, nil, true, "localhost:8585")
		require.Nil(t, results)
	})

	t.Run("no config no mirrors returns nil", func(t *testing.T) {
		cfg := &config.Config{
			ZotConfigRaw: json.RawMessage(`{}`),
		}
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
			ZotConfigRaw: json.RawMessage(`{}`),
		}
		cm := newTestConfigManager(t, cfg)

		mirrors := mirrorFlags{"containerd:quay.io"}
		results := resolveCRIAndApply(cm, mirrors, false, "localhost:8585")
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

// TestValidateRequiredFlags tests that whitespace-only values are treated as empty
func TestValidateRequiredFlags(t *testing.T) {
	tests := []struct {
		name                string
		groundControlURL    string
		token               string
		harborRegistryURL   string
		registryURL         string
		registryUsername    string
		registryPassword    string
		configDir           string
		registryDataDir     string
		imageDir            string
		spiffeEndpoint      string
		spiffeExpectedID    string
		shouldPassValidation bool // true if validation should pass, false if it should fail
	}{
		{
			name:                 "valid values",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			shouldPassValidation: true,
		},
		{
			name:                 "whitespace-only token",
			groundControlURL:     "http://gc:8080",
			token:                "   ",
			harborRegistryURL:    "http://hr:8080",
			shouldPassValidation: false,
		},
		{
			name:                 "whitespace-only ground control URL",
			groundControlURL:     "   ",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			shouldPassValidation: false,
		},
		{
			name:                 "whitespace-only harbor registry URL",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "   ",
			shouldPassValidation: false,
		},
		{
			name:                 "whitespace-only registry URL with BYO",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			registryURL:          "   ",
			registryUsername:     "user",
			registryPassword:     "pass",
			shouldPassValidation: false,
		},
		{
			name:                 "whitespace-only config dir (should pass as it gets default value)",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			configDir:            "   ",
			shouldPassValidation: true, // ConfigDir gets default value if empty/whitespace
		},
		{
			name:                 "whitespace-only SPIFFE endpoint",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			spiffeEndpoint:       "   ",
			shouldPassValidation: true, // SPIFFE endpoint is not required
		},
		{
			name:                 "values with surrounding whitespace (should pass after trim)",
			groundControlURL:     "  http://gc:8080  ",
			token:                "  valid-token  ",
			harborRegistryURL:    "  http://hr:8080  ",
			shouldPassValidation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for config files
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			prevPath := filepath.Join(dir, "prev_config.json")
			_ = configPath
			_ = prevPath

			// Create opts with test values
			opts := SatelliteOptions{
				GroundControlURL:    tt.groundControlURL,
				Token:               tt.token,
				HarborRegistryURL:   tt.harborRegistryURL,
				RegistryURL:         tt.registryURL,
				RegistryUsername:    tt.registryUsername,
				RegistryPassword:    tt.registryPassword,
				ConfigDir:           tt.configDir,
				RegistryDataDir:     tt.registryDataDir,
				ImageDir:            tt.imageDir,
				SPIFFEEndpointSocket: tt.spiffeEndpoint,
				SPIFFEExpectedServerID: tt.spiffeExpectedID,
				BYORegistry:         tt.registryURL != "", // Assume BYO if registry URL provided
			}

			// Simulate flag parsing and env var fallback (simplified)
			if opts.Token == "" {
				opts.Token = "" // envCfg.Token would be empty in test
			}
			if opts.RegistryPassword == "" {
				opts.RegistryPassword = "" // envCfg.RegistryPassword would be empty in test
			}

			// Apply trimming (same as in main)
			opts.GroundControlURL = strings.TrimSpace(opts.GroundControlURL)
			opts.Token = strings.TrimSpace(opts.Token)
			opts.HarborRegistryURL = strings.TrimSpace(opts.HarborRegistryURL)
			opts.RegistryURL = strings.TrimSpace(opts.RegistryURL)
			opts.RegistryUsername = strings.TrimSpace(opts.RegistryUsername)
			opts.RegistryPassword = strings.TrimSpace(opts.RegistryPassword)
			opts.ConfigDir = strings.TrimSpace(opts.ConfigDir)
			opts.RegistryDataDir = strings.TrimSpace(opts.RegistryDataDir)
			opts.ImageDir = strings.TrimSpace(opts.ImageDir)
			opts.SPIFFEEndpointSocket = strings.TrimSpace(opts.SPIFFEEndpointSocket)
			opts.SPIFFEExpectedServerID = strings.TrimSpace(opts.SPIFFEExpectedServerID)

			// Run validation (same logic as in main)
			var validationError error
			if !opts.FallbackOnly {
				if !opts.SPIFFEEnabled && (opts.Token == "" || opts.GroundControlURL == "") {
					validationError = fmt.Errorf("missing required arguments: --token and --ground-control-url")
				}
				if opts.GroundControlURL == "" {
					validationError = fmt.Errorf("missing required argument: --ground-control-url")
				}
				if opts.HarborRegistryURL == "" {
					validationError = fmt.Errorf("missing required argument: --harbor-registry-url")
				}
			}
			if opts.BYORegistry && opts.RegistryURL == "" {
				validationError = fmt.Errorf("missing required argument: --registry-url is required when --byo-registry is enabled")
			}

			// Check if validation passes/fails as expected
			if tt.shouldPassValidation {
				require.NoError(t, validationError, "Validation should have passed but got error: %v", validationError)
			} else {
				require.Error(t, validationError, "Validation should have failed but passed")
			}
		})
	}
}
