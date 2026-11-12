package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.JournalRepo = (*JournalRepo)(nil)

// JournalRepo is a recording fleet.JournalRepo.
type JournalRepo struct {
	rec *Recorder
	// Cursor answers OpenCursor.
	Cursor fleet.JournalCursor
}

func (j *JournalRepo) OpenCursor(_ context.Context, satellite fleet.SatelliteID, epoch int64) (fleet.JournalCursor, error) {
	if err := j.rec.Record("Journal.OpenCursor", satellite, epoch); err != nil {
		return fleet.JournalCursor{}, err
	}
	return j.Cursor, nil
}

func (j *JournalRepo) Advance(_ context.Context, satellite fleet.SatelliteID, epoch, ackedSeq int64, events []fleet.EdgeEvent) error {
	return j.rec.Record("Journal.Advance", satellite, epoch, ackedSeq, events)
}

// Advanced returns the events of every recorded Advance, failed ones
// included, in order.
func (j *JournalRepo) Advanced() []fleet.EdgeEvent {
	var out []fleet.EdgeEvent
	for _, c := range j.rec.CallsTo("Journal.Advance") {
		if events, ok := c.Args[3].([]fleet.EdgeEvent); ok {
			out = append(out, events...)
		}
	}
	return out
}
