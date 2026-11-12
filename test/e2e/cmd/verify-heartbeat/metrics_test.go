package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHeartbeatView(t *testing.T) {
	view, err := parseHeartbeatView(`# HELP satellite_heartbeats_total Heartbeats sent to Ground Control.
# TYPE satellite_heartbeats_total counter
satellite_heartbeats_total{code="200"} 5
satellite_heartbeats_total{code="500"} 2
satellite_heartbeats_total{code="error"} 1
satellite_journal_acks_total{outcome="accepted"} 3
satellite_journal_acks_total{outcome="rejected"} 1
satellite_journal_acked_seq 9
satellite_replication_bytes_total{outcome="ok"} 1024
`)
	require.NoError(t, err)
	require.InDelta(t, 5, view.ok, 0)
	require.InDelta(t, 3, view.refused, 0)
	require.InDelta(t, 3, view.accepted, 0)
	require.InDelta(t, 1, view.rejected, 0)
	require.Zero(t, view.dropped)
}
