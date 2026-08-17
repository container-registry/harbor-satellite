package main

import (
	"encoding/json"
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

// TestValidateAndTrimOptions tests the validation and trimming of satellite options
func TestValidateAndTrimOptions(t *testing.T) {
	tests := []struct {
		name                 string
		groundControlURL     string
		token                string
		harborRegistryURL    string
		registryURL          string
		registryUsername     string
		registryPassword     string
		configDir            string
		registryDataDir      string
		imageDir             string
		spiffeEndpoint       string
		spiffeExpectedID     string
		shutdownTimeout      string
		spiffeEnabled        bool
		byoRegistry          bool
		shouldPassValidation bool
		expectedGroundControlURL string
		expectedToken        string
		expectedHarborRegistryURL string
		expectedRegistryURL string
		expectedRegistryUsername string
		expectedRegistryPassword string
		expectedConfigDir string
		expectedRegistryDataDir string
		expectedImageDir string
		expectedSPIFFEEndpoint string
		expectedSPIFFEExpectedID string
		expectedShutdownTimeout string
	}{
		{
			name:                 "valid values with surrounding whitespace",
			groundControlURL:     "  http://gc:8080  ",
			token:                "  valid-token  ",
			harborRegistryURL:    "  http://hr:8080  ",
			registryURL:          "  http://reg:5000  ",
			registryUsername:     "  user  ",
			registryPassword:     "  pass with spaces  ",
			configDir:            "  /custom/config  ",
			registryDataDir:      "  /custom/data  ",
			imageDir:             "  /custom/images  ",
			spiffeEndpoint:       "  /tmp/socket.sock  ",
			spiffeExpectedID:     "  expected-id  ",
			shutdownTimeout:      "  45s  ",
			spiffeEnabled:        false,
			byoRegistry:          true,
			shouldPassValidation: true,
			expectedGroundControlURL: "http://gc:8080",
			expectedToken:        "valid-token",
			expectedHarborRegistryURL: "http://hr:8080",
			expectedRegistryURL: "http://reg:5000",
			expectedRegistryUsername: "user",
			expectedRegistryPassword: "  pass with spaces  ", // Password should NOT be trimmed
			expectedConfigDir: "/custom/config",
			expectedRegistryDataDir: "/custom/data",
			expectedImageDir: "/custom/images",
			expectedSPIFFEEndpoint: "/tmp/socket.sock",
			expectedSPIFFEExpectedID: "expected-id",
			expectedShutdownTimeout: "45s",
		},
		{
			name:                 "whitespace-only required fields should fail",
			groundControlURL:     "   ",
			token:                "   ",
			harborRegistryURL:    "   ",
			registryURL:          "",
			registryUsername:     "",
			registryPassword:     "",
			configDir:            "",
			registryDataDir:      "",
			imageDir:             "",
			spiffeEndpoint:       "",
			spiffeExpectedID:     "",
			shutdownTimeout:      "",
			spiffeEnabled:        false,
			byoRegistry:          false,
			shouldPassValidation: false,
			expectedGroundControlURL: "",
			expectedToken:        "",
			expectedHarborRegistryURL: "",
			expectedRegistryURL: "",
			expectedRegistryUsername: "",
			expectedRegistryPassword: "",
			expectedConfigDir: "",
			expectedRegistryDataDir: "",
			expectedImageDir: "",
			expectedSPIFFEEndpoint: "",
			expectedSPIFFEExpectedID: "",
			expectedShutdownTimeout: "",
		},
		{
			name:                 "whitespace-only token with valid env token should fall back",
			groundControlURL:     "http://gc:8080",
			token:                "   ", // whitespace-only CLI token
			harborRegistryURL:    "http://hr:8080",
			registryURL:          "",
			registryUsername:     "",
			registryPassword:     "",
			configDir:            "",
			registryDataDir:      "",
			imageDir:             "",
			spiffeEndpoint:       "",
			spiffeExpectedID:     "",
			shutdownTimeout:      "",
			spiffeEnabled:        false,
			byoRegistry:          false,
			shouldPassValidation: true, // Should fall back to env token
			expectedGroundControlURL: "http://gc:8080",
			expectedToken:        "env-token", // This will be set in test setup
			expectedHarborRegistryURL: "http://hr:8080",
			expectedRegistryURL: "",
			expectedRegistryUsername: "",
			expectedRegistryPassword: "",
			expectedConfigDir: "",
			expectedRegistryDataDir: "",
			expectedImageDir: "",
			expectedSPIFFEEndpoint: "",
			expectedSPIFFEExpectedID: "",
			expectedShutdownTimeout: "",
		},
		{
			name:                 "whitelist SPIFFE mode skips token/gc-url validation",
			groundControlURL:     "   ",
			token:                "   ",
			harborRegistryURL:    "http://hr:8080",
			registryURL:          "",
			registryUsername:     "",
			registryPassword:     "",
			configDir:            "",
			registryDataDir:      "",
			imageDir:             "",
			spiffeEndpoint:       "",
			spiffeExpectedID:     "",
			shutdownTimeout:      "",
			spiffeEnabled:        true,
			byoRegistry:          false,
			shouldPassValidation: true, // SPIFFE mode doesn't require token/gc-url
			expectedGroundControlURL: "",
			expectedToken:        "",
			expectedHarborRegistryURL: "http://hr:8080",
			expectedRegistryURL: "",
			expectedRegistryUsername: "",
			expectedRegistryPassword: "",
			expectedConfigDir: "",
			expectedRegistryDataDir: "",
			expectedImageDir: "",
			expectedSPIFFEEndpoint: "",
			expectedSPIFFEExpectedID: "",
			expectedShutdownTimeout: "",
		},
		{
			name:                 "BYO registry with whitespace-only registry URL should fail",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			registryURL:          "   ",
			registryUsername:     "user",
			registryPassword:     "pass",
			configDir:            "",
			registryDataDir:      "",
			imageDir:             "",
			spiffeEndpoint:       "",
			spiffeExpectedID:     "",
			shutdownTimeout:      "",
			spiffeEnabled:        false,
			byoRegistry:          true,
			shouldPassValidation: false,
			expectedGroundControlURL: "http://gc:8080",
			expectedToken:        "valid-token",
			expectedHarborRegistryURL: "http://hr:8080",
			expectedRegistryURL: "",
			expectedRegistryUsername: "user",
			expectedRegistryPassword: "pass",
			expectedConfigDir: "",
			expectedRegistryDataDir: "",
			expectedImageDir: "",
			expectedSPIFFEEndpoint: "",
			expectedSPIFFEExpectedID: "",
			expectedShutdownTimeout: "",
		},
		{
			name:                 "empty shutdown timeout should be trimmed",
			groundControlURL:     "http://gc:8080",
			token:                "valid-token",
			harborRegistryURL:    "http://hr:8080",
			registryURL:          "",
			registryUsername:     "",
			registryPassword:     "",
			configDir:            "",
			registryDataDir:      "",
			imageDir:             "",
			spiffeEndpoint:       "",
			spiffeExpectedID:     "",
			shutdownTimeout:      "   ",
			spiffeEnabled:        false,
			byoRegistry:          false,
			shouldPassValidation: true, // shutdownTimeout is not required for validation
			expectedGroundControlURL: "http://gc:8080",
			expectedToken:        "valid-token",
			expectedHarborRegistryURL: "http://hr:8080",
			expectedRegistryURL: "",
			expectedRegistryUsername: "",
			expectedRegistryPassword: "",
			expectedConfigDir: "",
			expectedRegistryDataDir: "",
			expectedImageDir: "",
			expectedSPIFFEEndpoint: "",
			expectedSPIFFEExpectedID: "",
			expectedShutdownTimeout: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				SPIFFEEnabled:        tt.spiffeEnabled,
				BYORegistry:          tt.byoRegistry,
			}

			shutdownTimeout := tt.shutdownTimeout

			// For the environment fallback test, we need to simulate env values
			if tt.name == "whitespace-only token with valid env token should fall back" {
				// Simulate what happens in main() after flag parsing but before validation
				if strings.TrimSpace(opts.Token) == "" {
					opts.Token = "env-token" // This simulates envCfg.Token
				}
			}

			// Call the validation function
			err := validateAndTrimOptions(&opts, &shutdownTimeout)

			// Check if validation passes/fails as expected
			if tt.shouldPassValidation {
				require.NoError(t, err, "Validation should have passed but got error: %v", err)
				// Check that values were trimmed correctly
				require.Equal(t, tt.expectedGroundControlURL, opts.GroundControlURL, "GroundControlURL mismatch")
				require.Equal(t, tt.expectedToken, opts.Token, "Token mismatch")
				require.Equal(t, tt.expectedHarborRegistryURL, opts.HarborRegistryURL, "HarborRegistryURL mismatch")
				require.Equal(t, tt.expectedRegistryURL, opts.RegistryURL, "RegistryURL mismatch")
				require.Equal(t, tt.expectedRegistryUsername, opts.RegistryUsername, "RegistryUsername mismatch")
				require.Equal(t, tt.expectedRegistryPassword, opts.RegistryPassword, "RegistryPassword mismatch (should preserve whitespace)")
				require.Equal(t, tt.expectedConfigDir, opts.ConfigDir, "ConfigDir mismatch")
				require.Equal(t, tt.expectedRegistryDataDir, opts.RegistryDataDir, "RegistryDataDir mismatch")
				require.Equal(t, tt.expectedImageDir, opts.ImageDir, "ImageDir mismatch")
				require.Equal(t, tt.expectedSPIFFEEndpoint, opts.SPIFFEEndpointSocket, "SPIFFEEndpointSocket mismatch")
				require.Equal(t, tt.expectedSPIFFEExpectedID, opts.SPIFFEExpectedServerID, "SPIFFEExpectedServerID mismatch")
				require.Equal(t, tt.expectedShutdownTimeout, shutdownTimeout, "shutdownTimeout mismatch")
			} else {
				require.Error(t, err, "Validation should have failed but passed")
			}
		})
	}
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
