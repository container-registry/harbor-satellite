package async

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// StaleDetector marks online satellites stale (plan item 4.3, ADR-0006).
// It runs in the worker; Views().MarkStale takes a singleton lock, so any
// number of workers may run it.
type StaleDetector struct {
	repo     fleet.Repository
	clock    fleet.Clock
	settings fleet.Settings
	interval time.Duration
}

var _ Job = (*StaleDetector)(nil)

func NewStaleDetector(repo fleet.Repository, clock fleet.Clock, settings fleet.Settings, interval time.Duration) (*StaleDetector, error) {
	if repo == nil || interval <= 0 {
		return nil, fmt.Errorf("%w: the stale detector needs a repository and a positive interval", fleet.ErrInvalid)
	}
	if settings.StaleThreshold <= 0 {
		return nil, fmt.Errorf("%w: STALE_THRESHOLD must be positive", fleet.ErrInvalid)
	}
	if clock == nil {
		clock = fleet.SystemClock{}
	}
	return &StaleDetector{repo: repo, clock: clock, settings: settings, interval: interval}, nil
}

func (d *StaleDetector) Name() string { return "stale-detector" }

// Run calls RunOnce every interval until ctx ends.
func (d *StaleDetector) Run(ctx context.Context) error {
	every(ctx, d.interval, func(ctx context.Context) {
		stale, err := d.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("stale detector: %v", err)
		case len(stale) > 0:
			log.Printf("stale detector: %d satellites stale", len(stale))
		}
	})
	return nil
}

// RunOnce runs Views().MarkStale(now, settings.StaleThreshold,
// 2*settings.HeartbeatFlushInterval) and appends one log-only SatelliteStale
// per satellite in the same transaction.
func (d *StaleDetector) RunOnce(ctx context.Context) ([]fleet.StaleSatellite, error) {
	var stale []fleet.StaleSatellite
	err := d.repo.WithTx(ctx, func(tx fleet.Tx) error {
		var err error
		stale, err = tx.Views().MarkStale(ctx, d.clock.Now(), d.settings.StaleThreshold, 2*d.settings.HeartbeatFlushInterval)
		if err != nil {
			return fmt.Errorf("mark stale satellites: %w", err)
		}
		for _, s := range stale {
			if err := tx.Events().Append(ctx, fleet.Event{
				Kind:      fleet.EventSatelliteStale,
				Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(s.Satellite)},
				Payload:   fleet.SatelliteLifecyclePayload{Name: s.Name, State: fleet.StateStale, PreviousState: fleet.StateOnline, LastSeen: s.LastSeen},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return stale, nil
}
