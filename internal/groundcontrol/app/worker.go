package app

import (
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
func NewWorker(deps WorkerDeps) (*async.Worker, error) {
	return nil, notimpl.Err("2.4 worker composition")
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
