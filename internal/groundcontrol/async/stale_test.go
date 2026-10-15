package async_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingStale = "ADR-0006 lifecycle is not implemented; stale satellites are computed per request"

var staleNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func staleSettings() fleet.Settings {
	s := settings
	s.StaleThreshold = 15 * time.Minute
	s.HeartbeatFlushInterval = 5 * time.Second
	return s
}

func newStaleDetector(t *testing.T, p *plane) *async.StaleDetector {
	t.Helper()
	d, err := async.NewStaleDetector(p.repo, fleettest.Clock{T: staleNow}, staleSettings(), time.Minute)
	require.NoError(t, err)
	return d
}

func TestStaleDetector_MarksAndAppendsLogOnlyEvents(t *testing.T) {
	pending.Plan(t, "4.3", pendingStale)
	p := newPlane()
	p.repo.Views.Stale = []fleet.StaleSatellite{
		{Satellite: 7, Name: "edge-01", LastSeen: staleNow.Add(-time.Hour)},
		{Satellite: 8, Name: "edge-02", LastSeen: staleNow.Add(-2 * time.Hour)},
	}

	got, err := newStaleDetector(t, p).RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, p.repo.Views.Stale, got)
	events := p.repo.Events.Appended()
	require.Len(t, events, 2)
	for i, e := range events {
		require.Equal(t, fleet.EventSatelliteStale, e.Kind)
		require.True(t, e.Kind.LogOnly())
		require.Equal(t, fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(p.repo.Views.Stale[i].Satellite)}, e.Aggregate)
		require.Equal(t, fleet.SatelliteLifecyclePayload{
			Name: p.repo.Views.Stale[i].Name, State: fleet.StateStale, PreviousState: fleet.StateOnline, LastSeen: p.repo.Views.Stale[i].LastSeen,
		}, e.Payload)
	}
	insideTx(t, p.rec, "Views.MarkStale")
	insideTx(t, p.rec, "Events.Append")
	require.Equal(t, 1, p.rec.Count("Commit"))
}

func TestStaleDetector_PassesThresholdAndFloor(t *testing.T) {
	pending.Plan(t, "4.3", pendingStale)
	p := newPlane()

	got, err := newStaleDetector(t, p).RunOnce(context.Background())

	require.NoError(t, err)
	require.Empty(t, got)
	mark := p.rec.CallsTo("Views.MarkStale")
	require.Len(t, mark, 1)
	require.Equal(t, []any{staleNow, 15 * time.Minute, 10 * time.Second}, mark[0].Args, "the floor is twice the flush interval")
	require.Empty(t, p.repo.Events.Appended())
}

// insideTx asserts that every call of method happens between a WithTx and
// its Commit.
func insideTx(t *testing.T, rec *fleettest.Recorder, method string) {
	t.Helper()
	inTx, seen := false, false
	for _, m := range rec.Methods() {
		switch m {
		case "WithTx":
			inTx = true
		case "Commit", "Rollback":
			inTx = false
		case method:
			seen = true
			require.True(t, inTx, "%s runs outside a transaction: %v", method, rec.Methods())
		}
	}
	require.True(t, seen, "%s not called: %v", method, rec.Methods())
}
