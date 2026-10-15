package app_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// The composed services write to the database and the Harbor they were
// given, not to the process-wide env.GC Harbor.
func TestNewFleet_ServicesUseTheInjectedDatabaseAndHarbor(t *testing.T) {
	pending.Plan(t, "1.3", "fleet composition is a stub; serve builds no services")
	db := dbtest.New(t)
	stub := harbortest.New(t)

	services, err := app.NewFleet(app.FleetDeps{
		Pools:  &dbpool.Pools{API: db.DB, Fleet: db.DB, Async: db.DB},
		Harbor: env.Harbor{URL: stub.URL(), Username: harbortest.Username, Password: harbortest.Password, RobotDurationDays: "30"},
	})
	require.NoError(t, err)

	_, err = services.Configs.Create(context.Background(), fleet.CreateConfigCmd{Name: "edge-config", Document: json.RawMessage(`{"app_config":{}}`)})
	require.NoError(t, err)

	require.True(t, stub.HasRepository("satellite/config-state/edge-config/state"))
	var name string
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT config_name FROM configs").Scan(&name))
	require.Equal(t, "edge-config", name)
}

func TestSettings_FromTheHarborEnvironment(t *testing.T) {
	got := app.Settings(env.Harbor{URL: "https://harbor.example", RobotDurationDays: "7"})

	require.Equal(t, fleet.Settings{RegistryURL: "https://harbor.example", RobotDuration: 7 * 24 * time.Hour, TokenTTL: fleet.DefaultTokenTTL}, got)
	require.Equal(t, 30*24*time.Hour, app.Settings(env.Harbor{RobotDurationDays: "nonsense"}).RobotDuration, "ROBOT_DURATION_DAYS falls back to 30")
}
