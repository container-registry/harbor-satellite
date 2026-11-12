package server

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/satconfig"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/secrets"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/harbor2"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
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

// hotPath composes the satellite hot path of plan item 4 as app.NewFleet
// does: sessions on the postgres session store, configured from
// SATELLITE_SESSION_* on clock, and a heartbeat buffer with its flusher.
// With flushEachBeat every beat is flushed in its request.
type hotPath struct {
	t             testing.TB
	clock         *manualClock
	flushEachBeat bool
	flusher       *heartbeat.Flusher
	// rotations and rotator exist with ROBOT_ROTATION_ENABLED (plan item 8.4).
	rotations *async.RotationCache
	rotator   *async.RobotRotator
}

func (h *hotPath) factory(_ context.Context, pools *dbpool.Pools, _ *spiffe.ServerClient) ([]Option, error) {
	srv := env.GC.Server
	store, err := postgres.NewSessionStore(pools.Fleet)
	if err != nil {
		return nil, err
	}
	sessions, err := authn.NewSessions(store, h.clock, authn.SessionConfig{
		TTL: srv.SatelliteSessionTTL, CacheTTL: srv.SatelliteSessionCacheTTL, CacheSize: srv.SatelliteSessionCacheSize,
	})
	if err != nil {
		return nil, err
	}
	cfg := heartbeat.Config{FlushInterval: srv.HeartbeatFlushInterval, MaxPending: srv.HeartbeatBufferMax, MaxFlushFailures: srv.HeartbeatMaxFlushFailures, FinalFlushTimeout: 5 * time.Second}
	buffer, err := heartbeat.NewBuffer(cfg)
	if err != nil {
		return nil, err
	}
	beats, err := postgres.NewHeartbeatStore(pools.Fleet, nil)
	if err != nil {
		return nil, err
	}
	if h.flusher, err = heartbeat.NewFlusher(buffer, beats, cfg); err != nil {
		return nil, err
	}
	var sink fleet.HeartbeatSink = buffer
	if h.flushEachBeat {
		sink = flushingSink{t: h.t, Buffer: buffer, flusher: h.flusher}
	}
	repo, err := postgres.NewRepository(pools.API)
	if err != nil {
		return nil, err
	}
	fleetRepo, err := postgres.NewRepository(pools.Fleet)
	if err != nil {
		return nil, err
	}
	var rotationSource fleet.RotationSource
	if env.GC.RobotRotation.Enabled {
		if err := h.composeRotation(pools); err != nil {
			return nil, err
		}
		rotationSource = h.rotations
	}
	svc, err := fleet.NewServices(fleet.Deps{
		Repository: repo, SatelliteRepository: fleetRepo, Registry: harnessRegistry(h.t, harborStub), Settings: harnessSettings(harborStub),
		Sessions: sessions, HeartbeatSink: sink, ConfigValidator: satconfig.Validator{}, Rotations: rotationSource,
	})
	if err != nil {
		return nil, err
	}
	return []Option{WithFleet(svc), WithSatelliteAuthenticator(sessions)}, nil
}

// flushingSink writes each beat before the request answers, as the sync
// handler did before plan item 4.2.
type flushingSink struct {
	t testing.TB
	*heartbeat.Buffer
	flusher *heartbeat.Flusher
}

func (s flushingSink) Record(beat fleet.Heartbeat) {
	s.Buffer.Record(beat)
	if err := s.flusher.Flush(context.Background()); err != nil {
		s.t.Errorf("flush heartbeat: %v", err)
	}
}

// composeRotation builds the rotation cache of the serve process and the
// worker's rotator, as app.NewFleet and app.NewWorker do.
func (h *hotPath) composeRotation(pools *dbpool.Pools) error {
	rot := env.GC.RobotRotation
	key, err := secrets.ParseKey(env.GC.DataEncryptionKey)
	if err != nil {
		return err
	}
	sealer, err := secrets.NewAESGCM(key)
	if err != nil {
		return err
	}
	store, err := postgres.NewRotationStore(pools.Fleet)
	if err != nil {
		return err
	}
	if h.rotations, err = async.NewRotationCache(store, sealer, rot.CacheRefresh); err != nil {
		return err
	}
	locker, err := postgres.NewAggregateLocker(context.Background(), pools.Fleet)
	if err != nil {
		return err
	}
	h.t.Cleanup(func() { _ = locker.Close() })
	repo, err := postgres.NewRepository(pools.Fleet)
	if err != nil {
		return err
	}
	h.rotator, err = async.NewRobotRotator(store, repo, harnessRegistry(h.t, harborStub), locker, sealer, fleet.SystemClock{},
		harnessSettings(harborStub).RobotDuration, rot.BeforeExpiry, rot.Interval)
	return err
}
