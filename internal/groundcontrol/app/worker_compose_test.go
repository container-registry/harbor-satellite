package app_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

var testKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

func harborEnv(stub *harbortest.Stub) env.Harbor {
	return env.Harbor{URL: stub.URL(), Username: harbortest.Username, Password: harbortest.Password, RobotDurationDays: "30", ProbeInterval: 20 * time.Millisecond}
}

// The composed worker turns an API write into a published state artifact
// in Harbor: the outbox event the fleet wrote is picked up by the
// publisher and the config reconciler pushes the config state.
func TestNewWorker_PublishesWhatTheFleetWrote(t *testing.T) {
	db := dbtest.New(t)
	stub := harbortest.New(t)
	pools := &dbpool.Pools{API: db.DB, Fleet: db.DB, Async: db.DB}

	f, err := app.NewFleet(app.FleetDeps{Pools: pools, Harbor: harborEnv(stub)})
	require.NoError(t, err)
	cfg := async.DefaultPublisherConfig()
	cfg.PollInterval = 20 * time.Millisecond
	worker, closeWorker, err := app.NewWorker(context.Background(), app.WorkerDeps{
		Pools: pools, Harbor: harborEnv(stub), Config: cfg,
		Webhook: env.Webhook{Enabled: true}, DataEncryptionKey: testKey,
		RobotRotation: env.RobotRotation{Enabled: true},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	_, err = f.Services.Configs.Create(context.Background(), fleet.CreateConfigCmd{Name: "edge-config", Document: json.RawMessage(`{"app_config":{"log_level":"debug"}}`)})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return stub.HasRepository("satellite/config-state/edge-config/state") }, 20*time.Second, 50*time.Millisecond,
		"the worker never published the config state")
	require.Eventually(t, func() bool {
		var pending int
		require.NoError(t, db.QueryRowContext(context.Background(), "SELECT count(*) FROM event_outbox WHERE done_at IS NULL").Scan(&pending))
		return pending == 0
	}, 20*time.Second, 50*time.Millisecond, "the event stayed pending after it was published")

	cancel()
	require.NoError(t, <-done)
	require.NoError(t, closeWorker())
	require.Zero(t, db.Stats().InUse, "the worker's lock connections are returned on close")
}

func TestNewWorker_RejectsIncompleteDependencies(t *testing.T) {
	db := dbtest.New(t)
	stub := harbortest.New(t)
	pools := &dbpool.Pools{API: db.DB, Fleet: db.DB, Async: db.DB}

	tests := []struct {
		name string
		deps app.WorkerDeps
		want string
	}{
		{name: "no pools", deps: app.WorkerDeps{Harbor: harborEnv(stub)}, want: "the worker needs database pools"},
		{name: "no Harbor", deps: app.WorkerDeps{Pools: pools}, want: "HARBOR_URL"},
		{
			name: "rotation without key", deps: app.WorkerDeps{Pools: pools, Harbor: harborEnv(stub), RobotRotation: env.RobotRotation{Enabled: true}},
			want: "ROBOT_ROTATION_ENABLED: invalid: DATA_ENCRYPTION_KEY is not set",
		},
		{
			name: "webhooks without key", deps: app.WorkerDeps{Pools: pools, Harbor: harborEnv(stub), Webhook: env.Webhook{Enabled: true}},
			want: "WEBHOOKS_ENABLED: invalid: DATA_ENCRYPTION_KEY is not set",
		},
		{
			name: "webhooks with a short key", deps: app.WorkerDeps{Pools: pools, Harbor: harborEnv(stub), Webhook: env.Webhook{Enabled: true}, DataEncryptionKey: "c2hvcnQ="},
			want: "want 32",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := app.NewWorker(context.Background(), tt.deps)
			require.ErrorContains(t, err, tt.want)
		})
	}
	require.Zero(t, db.Stats().InUse, "a failed NewWorker kept a lock connection checked out")
}
