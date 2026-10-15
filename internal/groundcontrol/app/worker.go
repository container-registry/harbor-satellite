package app

import (
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
	Config async.PublisherConfig
}

// NewWorker builds the publisher over the postgres outbox and aggregate
// locker, gated by a registry probe, and the routes of the reconcilers
// and the tombstone handler over a harbor2.Registry limited to
// Harbor.RateLimit, with Settings(deps.Harbor).
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
