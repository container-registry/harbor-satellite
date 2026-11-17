package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The satellite scheduler parses only "@every <positive duration>"; any
// other line passed validation and then stopped the satellite at start.
func TestSchedulesAreEveryOnly(t *testing.T) {
	fields := map[string]func(*AppConfig) *string{
		"state_replication_interval":  func(a *AppConfig) *string { return &a.StateReplicationInterval },
		"register_satellite_interval": func(a *AppConfig) *string { return &a.RegisterSatelliteInterval },
		"heartbeat_interval":          func(a *AppConfig) *string { return &a.HeartbeatInterval },
	}
	defaults := AppConfig{StateReplicationInterval: DefaultFetchAndReplicateCronExpr, RegisterSatelliteInterval: DefaultZTRCronExpr, HeartbeatInterval: DefaultHeartbeatCronExpr}
	for _, value := range []string{"*/5 * * * *", "0 * * * *", "@hourly", "@daily", "@every 0s", "@every -1m", "@every", "every 30s"} {
		for field, at := range fields {
			app := AppConfig{GroundControlURL: "https://gc.example"}
			*at(&app) = value

			_, err := ValidateAppConfig(app)
			require.ErrorContains(t, err, field, "Ground Control refuses %s=%q", field, value)

			got, warnings, err := ValidateAndEnforceDefaults(&Config{AppConfig: app}, "")
			require.NoError(t, err)
			require.NotEmpty(t, warnings, "%s=%q", field, value)
			require.Equal(t, *at(&defaults), *at(&got.AppConfig), "the satellite runs %s=%q on the default", field, value)
		}
	}

	ok := AppConfig{StateReplicationInterval: "@every 1m", RegisterSatelliteInterval: "@every 00h00m05s", HeartbeatInterval: "@every 1h30m"}
	warnings, err := ValidateAppConfig(ok)
	require.NoError(t, err)
	require.Empty(t, warnings)
}
