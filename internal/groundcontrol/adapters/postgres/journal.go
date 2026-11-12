package postgres

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// journalRepo runs OpenJournalCursor and AdvanceJournalCursor, which
// appends the edge events and moves the cursor in one statement. Advance
// checks that every kind is a log-only edge kind before the statement
// (fleet.ErrInvalid otherwise); a result with moved 0 is fleet.ErrConflict.
type journalRepo struct{}

func (journalRepo) OpenCursor(ctx context.Context, satellite fleet.SatelliteID, epoch int64) (fleet.JournalCursor, error) {
	return fleet.JournalCursor{}, notimpl.Err("6.5 postgres open journal cursor")
}

func (journalRepo) Advance(ctx context.Context, satellite fleet.SatelliteID, epoch, ackedSeq int64, events []fleet.EdgeEvent) error {
	return notimpl.Err("6.5 postgres advance journal cursor")
}
