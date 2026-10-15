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

// The composed services write to the database they were given and leave
// Harbor to the async plane (plan item 2.5); the probe runs on the Harbor
// they were given, not on the process-wide env.GC one.
func TestNewFleet_ServicesUseTheInjectedDatabaseAndHarbor(t *testing.T) {
	pending.Plan(t, "2.5", "fleet composition is a stub; serve builds no services")
	db := dbtest.New(t)
	stub := harbortest.New(t)

	f, err := app.NewFleet(app.FleetDeps{
		Pools:  &dbpool.Pools{API: db.DB, Fleet: db.DB, Async: db.DB},
		Harbor: env.Harbor{URL: stub.URL(), Username: harbortest.Username, Password: harbortest.Password, RobotDurationDays: "30", ProbeInterval: 10 * time.Millisecond},
	})
	require.NoError(t, err)
	require.NotNil(t, f.Probe)
	require.NotEmpty(t, f.ServerOptions(&dbpool.Pools{API: db.DB, Fleet: db.DB, Async: db.DB}))

	_, err = f.Services.Configs.Create(context.Background(), fleet.CreateConfigCmd{Name: "edge-config", Document: json.RawMessage(`{"app_config":{}}`)})
	require.NoError(t, err)

	var name, kind string
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT config_name FROM configs").Scan(&name))
	require.Equal(t, "edge-config", name)
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT kind FROM event_outbox").Scan(&kind))
	require.Equal(t, string(fleet.EventConfigChanged), kind)
	require.Empty(t, stub.Calls(), "the request leaves Harbor to the async plane")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Probe.Run(ctx) }()
	require.Eventually(t, f.Probe.Reachable, 5*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestSettings_FromTheHarborEnvironment(t *testing.T) {
	srv := env.Server{StaleThreshold: 15 * time.Minute, FleetHotPath: env.FleetHotPath{HeartbeatFlushInterval: 5 * time.Second}}
	got := app.Settings(env.Harbor{URL: "https://harbor.example", RobotDurationDays: "7"}, srv)

	require.Equal(t, fleet.Settings{
		RegistryURL: "https://harbor.example", RobotDuration: 7 * 24 * time.Hour, TokenTTL: fleet.DefaultTokenTTL,
		StaleThreshold: 15 * time.Minute, HeartbeatFlushInterval: 5 * time.Second,
	}, got)
	require.Equal(t, 30*24*time.Hour, app.Settings(env.Harbor{RobotDurationDays: "nonsense"}, srv).RobotDuration, "ROBOT_DURATION_DAYS falls back to 30")
}
