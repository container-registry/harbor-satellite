package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
)

type WorkerDeps struct {
	// Pools.Async carries the outbox, the desired state and the aggregate
	// locks, Pools.Fleet the reconcilers' reads.
	Pools  *dbpool.Pools
	Harbor env.Harbor
	Server env.Server
	Config async.PublisherConfig
	// RobotRotation and DataEncryptionKey configure the robot rotator of
	// plan item 8.4.
	RobotRotation     env.RobotRotation
	DataEncryptionKey string
	// Webhook configures the webhook dispatcher and OutboxRetention the
	// outbox pruner (plan item 9.1).
	Webhook         env.Webhook
	OutboxRetention time.Duration
	// SPIRE lets tombstones delete the SPIRE entries of deleted satellites;
	// nil leaves them.
	SPIRE *spiffe.ServerClient
}

// NewWorker builds the publisher over the postgres outbox and aggregate
// locker, gated by a registry probe, and the routes of the reconcilers
// and the tombstone handler over a harbor2.Registry limited to
// Harbor.RateLimit, with Settings(deps.Harbor, deps.Server, deps.Webhook). Its jobs of
// plan item 4 are async.StaleDetector (STALE_CHECK_INTERVAL),
// postgres.PartitionMaintainer (STATUS_RETENTION_DAYS,
// STATUS_PARTITIONS_AHEAD) and async.SessionExpirer over a
// postgres.SessionStore (SESSION_EXPIRY_INTERVAL). With
// ROBOT_ROTATION_ENABLED it adds async.RobotRotator over a
// postgres.RotationStore, sealing with secrets.AESGCM from
// DATA_ENCRYPTION_KEY (plan item 8.4). async.LoginStateExpirer over a
// postgres.IdentityStore deletes expired OIDC login states every
// SESSION_EXPIRY_INTERVAL (8.1). With WEBHOOKS_ENABLED it adds
// webhook.Dispatcher over the postgres WebhookStore, OutboxLog and
// SubscriptionLocker; async.OutboxPruner (OUTBOX_RETENTION) runs always,
// because it is the only retention of the journal's edge events (9.1).
//
// The 8.4, 8.1 and 9.1 jobs are added when those items land. It returns the locker's Close, which the
// caller runs after the worker stopped.
func NewWorker(ctx context.Context, deps WorkerDeps) (*async.Worker, func() error, error) {
	if deps.Pools == nil || deps.Pools.API == nil {
		return nil, nil, fmt.Errorf("%w: the worker needs database pools", fleet.ErrInvalid)
	}
	unlimited, err := newRegistry(deps.Harbor)
	if err != nil {
		return nil, nil, err
	}
	registry := async.LimitRegistry(unlimited, deps.Harbor.RateLimit)
	repo, err := postgres.NewRepository(fleetPool(deps.Pools))
	if err != nil {
		return nil, nil, err
	}
	pool := asyncPool(deps.Pools)
	outbox, err := postgres.NewOutboxStore(pool)
	if err != nil {
		return nil, nil, err
	}
	desired, err := postgres.NewDesiredStateStore(pool)
	if err != nil {
		return nil, nil, err
	}
	settings := Settings(deps.Harbor, deps.Server, deps.Webhook)
	satellites, err := async.NewSatelliteReconciler(repo, registry, desired, settings)
	if err != nil {
		return nil, nil, err
	}
	groups, err := async.NewGroupReconciler(repo, registry, desired, settings)
	if err != nil {
		return nil, nil, err
	}
	configs, err := async.NewConfigReconciler(repo, registry, desired, settings)
	if err != nil {
		return nil, nil, err
	}
	var identity fleet.WorkloadIdentity
	if deps.SPIRE != nil {
		if identity, err = spire.New(deps.SPIRE); err != nil {
			return nil, nil, err
		}
	}
	tombstones, err := async.NewTombstoneHandler(repo, registry, identity, desired)
	if err != nil {
		return nil, nil, err
	}
	routes, err := async.NewRoutes(satellites, groups, configs, tombstones)
	if err != nil {
		return nil, nil, err
	}
	interval := deps.Harbor.ProbeInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	probe, err := async.NewRegistryProbe(unlimited, interval, 3*interval)
	if err != nil {
		return nil, nil, err
	}
	locker, err := postgres.NewAggregateLocker(ctx, pool)
	if err != nil {
		return nil, nil, err
	}
	publisher, err := async.NewPublisher(async.PublisherDeps{Store: outbox, Handler: routes, Locker: locker, Registry: probe}, deps.Config)
	if err != nil {
		return nil, nil, errors.Join(err, locker.Close())
	}
	hot := withHotPathDefaults(deps.Server.FleetHotPath)
	stale, err := async.NewStaleDetector(repo, fleet.SystemClock{}, settings, hot.StaleCheckInterval)
	if err != nil {
		return nil, nil, errors.Join(err, locker.Close())
	}
	partitions, err := postgres.NewPartitionMaintainer(pool, fleet.SystemClock{}, hot.StatusRetentionDays, hot.StatusPartitionsAhead)
	if err != nil {
		return nil, nil, errors.Join(err, locker.Close())
	}
	sessionStore, err := postgres.NewSessionStore(pool)
	if err != nil {
		return nil, nil, errors.Join(err, locker.Close())
	}
	expirer, err := async.NewSessionExpirer(sessionStore, fleet.SystemClock{}, hot.SessionExpiryInterval)
	if err != nil {
		return nil, nil, errors.Join(err, locker.Close())
	}
	worker, err := async.NewWorker(publisher, probe, stale, partitions, expirer)
	if err != nil {
		return nil, nil, errors.Join(err, locker.Close())
	}
	return worker, locker.Close, nil
}

// PublisherConfig maps the OUTBOX_* settings.
func PublisherConfig(o env.Outbox) async.PublisherConfig {
	return async.PublisherConfig{
		BatchSize:    o.BatchSize,
		PollInterval: o.PollInterval,
		Concurrency:  o.Concurrency,
		MaxAttempts:  o.MaxAttempts,
		BackoffBase:  o.BackoffBase,
		BackoffMax:   o.BackoffMax,
		Lease:        o.Lease,
	}
}
