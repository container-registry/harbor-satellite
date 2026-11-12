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

	_, err = ParseByteRate("5GB")
	require.Error(t, err)
	_, err = ParseByteRate("9223372036854775808")
	require.Error(t, err)
}

func TestValidateNetworkConfig(t *testing.T) {
	cfg := &Config{AppConfig: AppConfig{GroundControlURL: "http://gc:8080"}}
	got, _, err := ValidateAndEnforceDefaults(cfg, "")
	require.NoError(t, err)
	require.False(t, got.AppConfig.Network.Speed.Enabled)

	cfg.AppConfig.Network = NetworkConfig{
		Speed: NetworkSpeedConfig{Enabled: true, Limit: "10Mbps"},
	}
	got, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.NoError(t, err)
	require.Equal(t, "10Mbps", got.AppConfig.Network.Speed.Limit)

	cfg.AppConfig.Network.Speed.Limit = "fast"
	_, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.ErrorContains(t, err, "network.speed.limit")

	cfg.AppConfig.Network.Speed.Limit = ""
	_, _, err = ValidateAndEnforceDefaults(cfg, "")
	require.ErrorContains(t, err, "network.speed.limit is required")

	disabled := &Config{AppConfig: AppConfig{
		GroundControlURL: "http://gc:8080",
		Network: NetworkConfig{
			Speed: NetworkSpeedConfig{Enabled: false, Limit: "fast"},
		},
	}}
	_, _, err = ValidateAndEnforceDefaults(disabled, "")
	require.NoError(t, err)
}
