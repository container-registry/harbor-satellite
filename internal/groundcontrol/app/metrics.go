package app

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/telemetry"
)

// countingSink counts every heartbeat the services accept into the buffer.
type countingSink struct {
	fleet.HeartbeatSink
	rec telemetry.Recorder
}

func (s countingSink) Record(beat fleet.Heartbeat) {
	s.HeartbeatSink.Record(beat)
	s.rec.CountHeartbeat()
}

// timedBeatStore records the outcome and duration of every flush,
// the per-satellite retries after a failed batch included.
type timedBeatStore struct {
	heartbeat.Store
	rec telemetry.Recorder
}

func (s timedBeatStore) Flush(ctx context.Context, batch heartbeat.Batch) error {
	start := time.Now()
	err := s.Store.Flush(ctx, batch)
	outcome := telemetry.OutcomeOK
	if err != nil {
		outcome = telemetry.OutcomeError
	}
	s.rec.ObserveHeartbeatFlush(outcome, time.Since(start))
	return err
}

// NewMetrics builds Ground Control's /metrics over the pools, the fleet's
// heartbeat buffer and the outbox on the async pool; events with
// maxAttempts or more attempts count as failed.
func NewMetrics(pools *dbpool.Pools, buffer *heartbeat.Buffer, maxAttempts int) (*telemetry.Server, error) {
	opts := telemetry.Options{}
	if pools != nil {
		opts.Pools = []telemetry.NamedPool{{Name: "api", DB: pools.API}, {Name: "fleet", DB: pools.Fleet}, {Name: "async", DB: pools.Async}}
		store, err := postgres.NewOutboxStore(asyncPool(pools))
		if err != nil {
			return nil, err
		}
		opts.Outbox = func(ctx context.Context) (telemetry.OutboxCounts, error) {
			stats, err := store.Stats(ctx, maxAttempts)
			return telemetry.OutboxCounts{Pending: stats.Pending, Failed: stats.Failed, OldestPending: stats.OldestPending}, err
		}
	}
	if buffer != nil {
		opts.HeartbeatBuffer = buffer.Len
	}
	return telemetry.NewServer(opts)
}
