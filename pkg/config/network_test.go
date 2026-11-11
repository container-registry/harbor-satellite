package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseNetworkQuantities(t *testing.T) {
	rate, err := ParseByteRate("10Mbps")
	require.NoError(t, err)
	require.Equal(t, int64(1_250_000), rate)

	rate, err = ParseByteRate("1MiB/s")
	require.NoError(t, err)
	require.Equal(t, int64(1024*1024), rate)

	size, err := ParseByteSize("5GB")
	require.NoError(t, err)
	require.Equal(t, int64(5_000_000_000), size)

	size, err = ParseByteSize("2048")
	require.NoError(t, err)
	require.Equal(t, int64(2048), size)

	_, err = ParseByteRate("5GB")
	require.Error(t, err)
	_, err = ParseByteSize("10Mbps")
	require.Error(t, err)
}

func TestValidateNetworkConfig(t *testing.T) {
	cfg := &Config{AppConfig: AppConfig{GroundControlURL: "http://gc:8080"}}
	got, _, err := ValidateAndEnforceDefaults(cfg, "")
	require.NoError(t, err)
	require.False(t, got.AppConfig.Network.Speed.Enabled)
	require.False(t, got.AppConfig.Network.Quota.Enabled)

	cfg.AppConfig.Network = NetworkConfig{
		Speed: NetworkSpeedConfig{Enabled: true, Limit: "10Mbps"},
		Quota: NetworkQuotaConfig{Enabled: true, Limit: "5GB"},
	}
	got, warnings, err := ValidateAndEnforceDefaults(cfg, "")
	require.NoError(t, err)
	require.Contains(t, warnings, "network.quota.window defaulted to day")
	require.Equal(t, "day", got.AppConfig.Network.Quota.Window)

	cfg.AppConfig.Network.Quota.Window = "month"
	_, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.ErrorContains(t, err, "network.quota.window")

	cfg.AppConfig.Network.Quota.Window = "week"
	cfg.AppConfig.Network.Speed.Limit = "fast"
	_, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.ErrorContains(t, err, "network.speed.limit")
}
