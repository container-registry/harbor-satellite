package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type validateTestCase struct {
	name           string
	config         *Config
	expectError    bool
	expectedErrMsg string
	expectWarnings bool
	expectedConfig *Config
}

func TestValidateAndEnforceDefaults(t *testing.T) {
	tests := []validateTestCase{
		{
			name: "valid config",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					LogLevel:                  "info",
					StateReplicationInterval:  "0 * * * *",
					RegisterSatelliteInterval: "*/5 * * * *",
				},
				ZotConfigRaw: []byte(`{"distSpecVersion":"1.1.0"}`),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name:           "nil config",
			config:         nil,
			expectError:    false,
			expectedErrMsg: "nil config",
			expectWarnings: true,
		},
		{
			name:           "empty config - defaults applied",
			config:         &Config{},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL(DefaultGroundControlURL),
					LogLevel:                  zerolog.LevelInfoValue,
					StateReplicationInterval:  DefaultFetchAndReplicateCronExpr,
					RegisterSatelliteInterval: DefaultZTRCronExpr,
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
		},
		{
			name: "invalid ground control URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("ht@tp://bad-url"),
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "empty zot config - fallback to default",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
				},
				ZotConfigRaw: []byte(""),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
		},
		{
			name: "invalid log level",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					LogLevel:         "badlevel",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					LogLevel: zerolog.LevelInfoValue,
				},
			},
		},
		{
			name: "invalid cron expressions",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					StateReplicationInterval:  "bad cron",
					RegisterSatelliteInterval: "also bad",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					StateReplicationInterval:  DefaultFetchAndReplicateCronExpr,
					RegisterSatelliteInterval: DefaultZTRCronExpr,
				},
			},
		},
		{
			name: "bring own registry missing URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						Username: "user",
						Password: "pass",
					},
				},
			},
			expectError:    true,
			expectedErrMsg: "custom registry URL is required",
		},
		{
			name: "bring own registry with invalid URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL:      URL("not-a-valid-url"),
						Username: "user",
						Password: "pass",
					},
				},
			},
			expectError:    true,
			expectedErrMsg: "invalid custom registry URL",
		},
		{
			name: "bring own registry with valid config",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL:      URL("https://myregistry.example.com"),
						Username: "user",
						Password: "pass",
					},
				},
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "bring own registry missing credentials warns",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL: URL("https://myregistry.example.com"),
					},
				},
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "bring own registry with redundant zot config warns",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL: URL("https://example.com"),
					BringOwnRegistry: true,
					LocalRegistryCredentials: RegistryCredentials{
						URL:      URL("https://myregistry.example.com"),
						Username: "user",
						Password: "pass",
					},
				},
				ZotConfigRaw: []byte(`{"distSpecVersion":"1.1.0"}`),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "invalid heartbeat interval defaults",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:  URL("https://example.com"),
					HeartbeatInterval: "invalid cron",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					HeartbeatInterval: DefaultHeartbeatCronExpr,
				},
			},
		},
		{
			name: "empty cron expression defaults",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					StateReplicationInterval:  "",
					RegisterSatelliteInterval: "",
					HeartbeatInterval:         "",
				},
				ZotConfigRaw: []byte(DefaultZotConfigJSON),
			},
			expectError:    false,
			expectWarnings: true,
			expectedConfig: &Config{
				AppConfig: AppConfig{
					StateReplicationInterval:  DefaultFetchAndReplicateCronExpr,
					RegisterSatelliteInterval: DefaultZTRCronExpr,
					HeartbeatInterval:         DefaultHeartbeatCronExpr,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, warnings, err := ValidateAndEnforceDefaults(tt.config, DefaultGroundControlURL)

			if tt.expectError {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					require.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				return
			}

			require.NoError(t, err)

			if tt.expectWarnings {
				require.NotEmpty(t, warnings)
			} else {
				require.Empty(t, warnings)
			}

			if tt.expectedConfig != nil {
				exp := tt.expectedConfig.AppConfig
				got := config.AppConfig
				if exp.LogLevel != "" {
					require.Equal(t, exp.LogLevel, got.LogLevel)
				}
				if exp.StateReplicationInterval != "" {
					require.Equal(t, exp.StateReplicationInterval, got.StateReplicationInterval)
				}
				if exp.RegisterSatelliteInterval != "" {
					require.Equal(t, exp.RegisterSatelliteInterval, got.RegisterSatelliteInterval)
				}
				if exp.HeartbeatInterval != "" {
					require.Equal(t, exp.HeartbeatInterval, got.HeartbeatInterval)
				}
				if len(tt.expectedConfig.ZotConfigRaw) > 0 {
					require.JSONEq(t, string(tt.expectedConfig.ZotConfigRaw), string(config.ZotConfigRaw))
				}
			}
		})
	}
}

func TestValidateTLSConfig(t *testing.T) {
	t.Run("valid TLS with cert and key files", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		keyFile := filepath.Join(tmpDir, "key.pem")

		require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0600))
		require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CertFile: certFile,
					KeyFile:  keyFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, warnings, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.NotContains(t, warnings, "cert_file")
	})

	t.Run("error when only cert_file provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CertFile: certFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "both cert_file and key_file must be provided")
	})

	t.Run("error when only key_file provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "key.pem")
		require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					KeyFile: keyFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "both cert_file and key_file must be provided")
	})

	t.Run("error when cert_file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "key.pem")
		require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CertFile: "/nonexistent/cert.pem",
					KeyFile:  keyFile,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cert_file not found")
	})

	t.Run("warning when skip_verify enabled with ca_file", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("ca"), 0600))

		config := &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
				TLS: TLSConfig{
					CAFile:     caFile,
					SkipVerify: true,
				},
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}

		_, warnings, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		found := false
		for _, w := range warnings {
			if w == "TLS skip_verify is enabled, certificate verification will be skipped" {
				found = true
				break
			}
		}
		require.True(t, found, "expected skip_verify warning")
	})
}

func TestUseUnsecureEnvVar(t *testing.T) {
	baseConfig := func() *Config {
		return &Config{
			AppConfig: AppConfig{
				GroundControlURL: URL("https://example.com"),
			},
			ZotConfigRaw: []byte(DefaultZotConfigJSON),
		}
	}

	t.Run("USE_UNSECURE=true sets UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "true")
		config := baseConfig()
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.True(t, result.AppConfig.UseUnsecure)
	})

	t.Run("USE_UNSECURE=1 sets UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "1")
		config := baseConfig()
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.True(t, result.AppConfig.UseUnsecure)
	})

	t.Run("USE_UNSECURE=false does not set UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "false")
		config := baseConfig()
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.False(t, result.AppConfig.UseUnsecure)
	})

	t.Run("USE_UNSECURE empty does not change UseUnsecure", func(t *testing.T) {
		t.Setenv("USE_UNSECURE", "")
		config := baseConfig()
		config.AppConfig.UseUnsecure = false
		result, _, err := ValidateAndEnforceDefaults(config, DefaultGroundControlURL)
		require.NoError(t, err)
		require.False(t, result.AppConfig.UseUnsecure)
	})
}
