package server

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/harbor2"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/stretchr/testify/require"
)

// harnessSettings are the fleet settings of a harness on stub.
func harnessSettings(stub *harbortest.Stub) fleet.Settings {
	return fleet.Settings{RegistryURL: stub.URL(), RobotDuration: 30 * 24 * time.Hour, TokenTTL: fleet.DefaultTokenTTL}
}

func harnessRegistry(t testing.TB, stub *harbortest.Stub) *harbor2.Registry {
	t.Helper()
	registry, err := harbor2.New(harbor2.Config{URL: stub.URL(), Username: harbortest.Username, Password: harbortest.Password, Insecure: true})
	require.NoError(t, err)
	return registry
}

// harnessServices composes the fleet services on db and harborStub, as
// app.NewFleet does, which this package's tests cannot import.
func harnessServices(t testing.TB, db *dbtest.DB) *fleet.Services {
	t.Helper()
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	svc, err := fleet.NewServices(fleet.Deps{Repository: repo, Registry: harnessRegistry(t, harborStub), Settings: harnessSettings(harborStub)})
	require.NoError(t, err)
	return svc
}

// pingReachability asks the registry on every call, so a drain sees Harbor
// go down at once.
type pingReachability struct{ registry fleet.Registry }

func (p pingReachability) Reachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return p.registry.Ping(ctx) == nil
}

// drainOutbox runs the publisher until no event is pending; it stops
// early while the registry does not answer.
func drainOutbox(t testing.TB, db *dbtest.DB, stub *harbortest.Stub) {
	t.Helper()
	ctx := context.Background()
	registry := harnessRegistry(t, stub)
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	outbox, err := postgres.NewOutboxStore(db.DB)
	require.NoError(t, err)
	desired, err := postgres.NewDesiredStateStore(db.DB)
	require.NoError(t, err)
	locker, err := postgres.NewAggregateLocker(ctx, db.DB)
	require.NoError(t, err)
	defer func() { require.NoError(t, locker.Close()) }()
	settings := harnessSettings(stub)
	sat, err := async.NewSatelliteReconciler(repo, registry, desired, settings)
	require.NoError(t, err)
	grp, err := async.NewGroupReconciler(repo, registry, desired, settings)
	require.NoError(t, err)
	cfg, err := async.NewConfigReconciler(repo, registry, desired, settings)
	require.NoError(t, err)
	tomb, err := async.NewTombstoneHandler(repo, registry, nil, desired)
	require.NoError(t, err)
	routes, err := async.NewRoutes(sat, grp, cfg, tomb)
	require.NoError(t, err)
	pubCfg := async.DefaultPublisherConfig()
	pubCfg.BackoffBase, pubCfg.BackoffMax = 10*time.Millisecond, 50*time.Millisecond
	publisher, err := async.NewPublisher(async.PublisherDeps{Store: outbox, Handler: routes, Locker: locker, Registry: pingReachability{registry}}, pubCfg)
	require.NoError(t, err)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !(pingReachability{registry}).Reachable() {
			return
		}
		if _, err := publisher.RunOnce(ctx); err != nil {
			t.Logf("drain outbox: %v", err)
		}
		pending, err := outbox.PendingCount(ctx)
		require.NoError(t, err)
		if pending == 0 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	stuck, err := outbox.Stuck(ctx, 0, 20)
	require.NoError(t, err)
	t.Fatalf("the outbox did not drain: %+v", stuck)
}
