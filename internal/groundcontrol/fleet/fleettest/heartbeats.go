package fleettest

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.HeartbeatSink = (*HeartbeatSink)(nil)

// HeartbeatSink is a recording fleet.HeartbeatSink; Record is recorded as
// "Heartbeats.Record" with the beat.
type HeartbeatSink struct {
	*Recorder
	// Digests answers InventoryDigest.
	Digests map[fleet.SatelliteID]fleet.Digest
}

// NewHeartbeatSink records into rec, or into a new Recorder when rec is
// nil.
func NewHeartbeatSink(rec *Recorder) *HeartbeatSink {
	return &HeartbeatSink{Recorder: orNew(rec)}
}

func (h *HeartbeatSink) Record(beat fleet.Heartbeat) {
	h.Note("Heartbeats.Record", beat)
}

func (h *HeartbeatSink) InventoryDigest(satellite fleet.SatelliteID) (fleet.Digest, bool) {
	h.Note("Heartbeats.InventoryDigest", satellite)
	d, ok := h.Digests[satellite]
	return d, ok
}

// Beats returns the recorded beats in order.
func (h *HeartbeatSink) Beats() []fleet.Heartbeat {
	var out []fleet.Heartbeat
	for _, c := range h.CallsTo("Heartbeats.Record") {
		if beat, ok := c.Args[0].(fleet.Heartbeat); ok {
			out = append(out, beat)
		}
	}
	return out
}
