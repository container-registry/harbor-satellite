package config

import (
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
	testRegistryDir := t.TempDir()
	expectedZotConfigJSON := `{
  "distSpecVersion": "1.1.0",
  "storage": {
    "rootDirectory": "` + testRegistryDir + `"
  },
  "http": {
    "address": "0.0.0.0",
    "port": "8585"
  },
  "log": {
    "level": "info"
  }
}`

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
				ZotConfigRaw: []byte(expectedZotConfigJSON),
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
			expectError:    true,
			expectedErrMsg: "invalid URL provided",
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
				ZotConfigRaw: []byte(expectedZotConfigJSON),
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, warnings, err := ValidateAndEnforceDefaults(tt.config, DefaultGroundControlURL, testRegistryDir)

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
				if len(tt.expectedConfig.ZotConfigRaw) > 0 {
					require.JSONEq(t, string(tt.expectedConfig.ZotConfigRaw), string(config.ZotConfigRaw))
				}
			}
		})
	}
}
