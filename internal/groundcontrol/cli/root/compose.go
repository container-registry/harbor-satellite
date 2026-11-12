package root

import (
	"context"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
)

// composition keeps what the server's fleet factory built, so serve can
// run the probe and the worker on the same pools.
type composition struct {
	fleet *app.Fleet
	pools *dbpool.Pools
	spire *spiffe.ServerClient
}

func (c *composition) fleetFactory(_ context.Context, pools *dbpool.Pools, spire *spiffe.ServerClient) ([]server.Option, error) {
	f, err := app.NewFleet(app.FleetDeps{
		Pools: pools, Harbor: env.GC.Harbor, Server: env.GC.Server, Webhook: env.GC.Webhook, SPIRE: spire, Policy: env.GC.Policy,
		RobotRotation: env.GC.RobotRotation, DataEncryptionKey: env.GC.DataEncryptionKey,
	})
	if err != nil {
		return nil, err
	}
	c.fleet, c.pools, c.spire = f, pools, spire
	return f.ServerOptions(pools), nil
}

func (c *composition) jobs(ctx context.Context, embedded bool) ([]job, error) {
	if c.fleet == nil {
		return nil, errors.New("the fleet was not composed")
	}
	jobs := []job{{name: c.fleet.Probe.Name(), run: c.fleet.Probe.Run}}
	if c.fleet.Flusher != nil {
		jobs = append(jobs, job{name: c.fleet.Flusher.Name(), run: c.fleet.Flusher.Run})
	}
	if c.fleet.Rotations != nil {
		jobs = append(jobs, job{name: c.fleet.Rotations.Name(), run: c.fleet.Rotations.Run})
	}
	if c.fleet.Policy != nil {
		jobs = append(jobs, job{name: c.fleet.Policy.Name(), run: c.fleet.Policy.Run})
	}
	if !embedded {
		return jobs, nil
	}
	w, closeWorker, err := newWorker(ctx, c.pools, c.spire)
	if err != nil {
		return nil, err
	}
	return append(jobs, job{name: "async worker", run: func(ctx context.Context) error {
		return errors.Join(w.Run(ctx), closeWorker())
	}}), nil
}

func newWorker(ctx context.Context, pools *dbpool.Pools, spire *spiffe.ServerClient) (interface{ Run(context.Context) error }, func() error, error) {
	w, closeWorker, err := app.NewWorker(ctx, app.WorkerDeps{
		Pools: pools, Harbor: env.GC.Harbor, Server: env.GC.Server, Config: app.PublisherConfig(env.GC.Outbox),
		Webhook: env.GC.Webhook, SPIRE: spire, RobotRotation: env.GC.RobotRotation, DataEncryptionKey: env.GC.DataEncryptionKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("compose the async worker: %w", err)
	}
	return w, closeWorker, nil
}
