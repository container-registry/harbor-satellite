package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/telemetry"
	"github.com/stretchr/testify/require"
)

type spyRecorder struct {
	telemetry.Nop
	beats   int
	flushes []string
}

func (s *spyRecorder) CountHeartbeat() { s.beats++ }
func (s *spyRecorder) ObserveHeartbeatFlush(outcome string, _ time.Duration) {
	s.flushes = append(s.flushes, outcome)
}

type nopSink struct{ recorded int }

func (n *nopSink) Record(fleet.Heartbeat)                                 { n.recorded++ }
func (n *nopSink) InventoryDigest(fleet.SatelliteID) (fleet.Digest, bool) { return "", false }

type failingStore struct{ err error }

func (f failingStore) Flush(context.Context, heartbeat.Batch) error { return f.err }

func TestCountingSink_CountsRecordedBeats(t *testing.T) {
	rec, sink := &spyRecorder{}, &nopSink{}
	s := countingSink{HeartbeatSink: sink, rec: rec}
	s.Record(fleet.Heartbeat{})
	s.Record(fleet.Heartbeat{})
	require.Equal(t, 2, sink.recorded)
	require.Equal(t, 2, rec.beats)
}

func TestTimedBeatStore_RecordsOutcome(t *testing.T) {
	rec := &spyRecorder{}
	require.NoError(t, timedBeatStore{Store: failingStore{}, rec: rec}.Flush(context.Background(), heartbeat.Batch{}))
	boom := errors.New("boom")
	require.ErrorIs(t, timedBeatStore{Store: failingStore{err: boom}, rec: rec}.Flush(context.Background(), heartbeat.Batch{}), boom)
	require.Equal(t, []string{telemetry.OutcomeOK, telemetry.OutcomeError}, rec.flushes)
}
