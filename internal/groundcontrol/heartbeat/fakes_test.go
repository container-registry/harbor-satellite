package heartbeat_test

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
)

var (
	start   = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

// store records every flushed batch and fails the satellites in poison.
type store struct {
	mu      sync.Mutex
	batches []heartbeat.Batch
	poison  map[fleet.SatelliteID]bool
	fails   int
}

func (s *store) Flush(_ context.Context, batch heartbeat.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range batch.Latest {
		if s.poison[b.Satellite] {
			s.fails++
			return errBoom
		}
	}
	s.batches = append(s.batches, batch)
	return nil
}

func (s *store) flushed() []heartbeat.Batch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]heartbeat.Batch(nil), s.batches...)
}

// satellites returns the satellites of every flushed Latest, in order.
func (s *store) satellites() []fleet.SatelliteID {
	var out []fleet.SatelliteID
	for _, b := range s.flushed() {
		for _, beat := range b.Latest {
			out = append(out, beat.Satellite)
		}
	}
	return out
}

func beatAt(satellite fleet.SatelliteID, offset time.Duration) fleet.Heartbeat {
	seen := start.Add(offset)
	return fleet.Heartbeat{
		Satellite: satellite,
		SeenAt:    seen,
		Interval:  "@every 00h00m30s",
		Sample:    fleet.StatusSample{Satellite: satellite, Activity: "idle", ReportedAt: seen},
	}
}
