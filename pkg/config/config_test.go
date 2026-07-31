package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestP2PConfigParsing(t *testing.T) {
	t.Run("default is false", func(t *testing.T) {
		var c Config
		err := json.Unmarshal([]byte(`{"app_config": {}}`), &c)
		require.NoError(t, err)
		require.False(t, c.AppConfig.P2P.Enabled)
	})

	t.Run("parses true and fields", func(t *testing.T) {
		var c Config
		err := json.Unmarshal([]byte(`{"app_config": {"p2p": {"enabled": true, "peers": ["https://peer1"], "acquisition_timeout": "60s", "max_concurrent_transfers": 10}}}`), &c)
		require.NoError(t, err)
		require.True(t, c.AppConfig.P2P.Enabled)
		require.Equal(t, []string{"https://peer1"}, c.AppConfig.P2P.Peers)
		require.Equal(t, 60*time.Second, c.AppConfig.P2P.AcquisitionTimeout)
		require.Equal(t, 10, c.AppConfig.P2P.MaxConcurrentTransfers)
	})
}

func TestValidateP2PConfig(t *testing.T) {
	tests := []struct {
		name          string
		config        func() *Config
		expectErr     bool
		expectWarning bool
	}{
		{
			name: "disabled returns nil",
			config: func() *Config {
				return &Config{}
			},
			expectErr:     false,
			expectWarning: false,
		},
		{
			name: "enabled with no peers returns error",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.AcquisitionTimeout = 10 * time.Second
				c.AppConfig.P2P.MaxConcurrentTransfers = 5
				return c
			},
			expectErr:     true,
			expectWarning: false,
		},
		{
			name: "enabled with invalid peer url returns error",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"not-a-url"}
				c.AppConfig.P2P.AcquisitionTimeout = 10 * time.Second
				c.AppConfig.P2P.MaxConcurrentTransfers = 5
				return c
			},
			expectErr:     true,
			expectWarning: false,
		},
		{
			name: "enabled with zero timeout returns specific error",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"https://peer1"}
				c.AppConfig.P2P.AcquisitionTimeout = 0
				c.AppConfig.P2P.MaxConcurrentTransfers = 5
				return c
			},
			expectErr:     true,
			expectWarning: false,
		},
		{
			name: "enabled with short timeout returns error",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"https://peer1"}
				c.AppConfig.P2P.AcquisitionTimeout = 1 * time.Second
				c.AppConfig.P2P.MaxConcurrentTransfers = 5
				return c
			},
			expectErr:     true,
			expectWarning: false,
		},
		{
			name: "enabled with long timeout returns error",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"https://peer1"}
				c.AppConfig.P2P.AcquisitionTimeout = 10 * time.Minute
				c.AppConfig.P2P.MaxConcurrentTransfers = 5
				return c
			},
			expectErr:     true,
			expectWarning: false,
		},
		{
			name: "enabled with zero concurrent transfers returns warning and defaults",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"https://peer1"}
				c.AppConfig.P2P.AcquisitionTimeout = 10 * time.Second
				c.AppConfig.P2P.MaxConcurrentTransfers = 0
				return c
			},
			expectErr:     false,
			expectWarning: true,
		},
		{
			name: "enabled with too many concurrent transfers returns error",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"https://peer1"}
				c.AppConfig.P2P.AcquisitionTimeout = 10 * time.Second
				c.AppConfig.P2P.MaxConcurrentTransfers = 50
				return c
			},
			expectErr:     true,
			expectWarning: false,
		},
		{
			name: "enabled with valid configuration succeeds",
			config: func() *Config {
				c := &Config{}
				c.AppConfig.P2P.Enabled = true
				c.AppConfig.P2P.Peers = []string{"https://peer1"}
				c.AppConfig.P2P.AcquisitionTimeout = 30 * time.Second
				c.AppConfig.P2P.MaxConcurrentTransfers = 10
				return c
			},
			expectErr:     false,
			expectWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config()
			warnings, err := validateP2PConfig(cfg)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.expectWarning {
				require.NotEmpty(t, warnings)
			} else {
				require.Empty(t, warnings)
			}
		})
	}
}
