package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// StaleDetector marks online satellites stale (plan item 4.3, ADR-0006).
// It runs in the worker; Views().MarkStale takes a singleton lock, so any
// number of workers may run it.
type StaleDetector struct{}

func NewStaleDetector(repo fleet.Repository, clock fleet.Clock, settings fleet.Settings, interval time.Duration) (*StaleDetector, error) {
	return nil, notimpl.Err("4.3 stale detector")
}

func (d *StaleDetector) Name() string { return "stale-detector" }

// Run calls RunOnce every interval until ctx ends.
func (d *StaleDetector) Run(ctx context.Context) error {
	return notimpl.Err("4.3 stale detector run")
}

// RunOnce runs Views().MarkStale(now, settings.StaleThreshold,
// 2*settings.HeartbeatFlushInterval) and appends one log-only SatelliteStale
// per satellite in the same transaction.
func (d *StaleDetector) RunOnce(ctx context.Context) ([]fleet.StaleSatellite, error) {
	return nil, notimpl.Err("4.3 stale detector run once")
}
