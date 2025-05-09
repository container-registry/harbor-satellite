package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         *Config
		expectError    bool
		expectedErrMsg string
		expectWarnings bool
	}{
		{
			name: "valid config",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					LogLevel:                  "info",
					StateReplicationInterval:  "0 * * * *",
					RegisterSatelliteInterval: "*/5 * * * *",
					UpdateConfigInterval:      "0 0 * * *",
				},
				ZotConfigRaw: []byte("{}"),
			},
			expectError:    false,
			expectWarnings: false,
		},
		{
			name:           "nil config",
			config:         nil,
			expectError:    true,
			expectedErrMsg: "config cannot be nil",
			expectWarnings: false,
		},
		{
			name:           "empty config",
			config:         &Config{},
			expectError:    true,
			expectedErrMsg: "config cannot be empty",
			expectWarnings: false,
		},
		{
			name: "invalid ground control URL",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("ht@tp://bad-url"),
					LogLevel:                  "info",
					StateReplicationInterval:  "0 * * * *",
					RegisterSatelliteInterval: "*/5 * * * *",
					UpdateConfigInterval:      "0 0 * * *",
				},
				ZotConfigRaw: []byte("{}"),
			},
			expectError:    true,
			expectedErrMsg: "invalid URL provided",
		},
		{
			name: "empty zot config",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					LogLevel:                  "info",
					StateReplicationInterval:  "0 * * * *",
					RegisterSatelliteInterval: "*/5 * * * *",
					UpdateConfigInterval:      "0 0 * * *",
				},
				ZotConfigRaw: []byte(""),
			},
			expectError:    true,
			expectedErrMsg: "invalid zot_config",
		},
		{
			name: "invalid log level",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					LogLevel:                  "badlevel",
					StateReplicationInterval:  "0 * * * *",
					RegisterSatelliteInterval: "*/5 * * * *",
					UpdateConfigInterval:      "0 0 * * *",
				},
				ZotConfigRaw: []byte("{}"),
			},
			expectError:    false,
			expectWarnings: true,
		},
		{
			name: "invalid cron expressions",
			config: &Config{
				AppConfig: AppConfig{
					GroundControlURL:          URL("https://example.com"),
					LogLevel:                  "info",
					StateReplicationInterval:  "bad cron expr",
					RegisterSatelliteInterval: "also bad",
					UpdateConfigInterval:      "not good",
				},
				ZotConfigRaw: []byte("{}"),
			},
			expectError:    false,
			expectWarnings: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings, err := ValidateConfig(tt.config)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrMsg)
				return
			}

			require.NoError(t, err)

			if tt.expectWarnings {
				require.NotEmpty(t, warnings, "expected warnings but got none")
			} else {
				require.Empty(t, warnings, "expected no warnings but got some")
			}
		})
	}
}
