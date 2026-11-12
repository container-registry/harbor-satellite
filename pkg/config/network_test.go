package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseQuotaQuantities(t *testing.T) {
	size, err := ParseByteSize("5GB")
	require.NoError(t, err)
	require.Equal(t, int64(5_000_000_000), size)

	size, err = ParseByteSize("2048")
	require.NoError(t, err)
	require.Equal(t, int64(2048), size)

	_, err = ParseByteSize("10Mbps")
	require.Error(t, err)
	_, err = ParseByteSize("9223372036854775808")
	require.Error(t, err)
}

func TestValidateQuotaConfig(t *testing.T) {
	cfg := &Config{AppConfig: AppConfig{GroundControlURL: "http://gc:8080"}}
	got, _, err := ValidateAndEnforceDefaults(cfg, "")
	require.NoError(t, err)
	require.False(t, got.AppConfig.Network.Quota.Enabled)

	cfg.AppConfig.Network = NetworkConfig{
		Quota: NetworkQuotaConfig{Enabled: true, Limit: "5GB"},
	}
	got, warnings, err := ValidateAndEnforceDefaults(cfg, "")
	require.NoError(t, err)
	require.Contains(t, warnings, "network.quota.window defaulted to day")
	require.Equal(t, "day", got.AppConfig.Network.Quota.Window)

	cfg.AppConfig.Network.Quota.Window = "month"
	_, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.ErrorContains(t, err, "network.quota.window")

	cfg.AppConfig.Network.Quota.Limit = ""
	cfg.AppConfig.Network.Quota.Window = "day"
	_, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.ErrorContains(t, err, "network.quota.limit is required")

	disabled := &Config{AppConfig: AppConfig{
		GroundControlURL: "http://gc:8080",
		Network: NetworkConfig{
			Quota: NetworkQuotaConfig{Enabled: false, Limit: "lots", Window: "month"},
		},
	}}
	_, _, err = ValidateAndEnforceDefaults(disabled, "")
	require.NoError(t, err)
}
