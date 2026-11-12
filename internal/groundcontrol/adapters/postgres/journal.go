package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// journalRepo runs OpenJournalCursor and AdvanceJournalCursor, which
// appends the edge events and moves the cursor in one statement. Advance
// checks that every kind is a log-only edge kind before the statement
// (fleet.ErrInvalid otherwise); a result with moved 0 is fleet.ErrConflict.
type journalRepo struct{ q *database.Queries }

func (r journalRepo) OpenCursor(ctx context.Context, satellite fleet.SatelliteID, epoch int64) (fleet.JournalCursor, error) {
	row, err := r.q.OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: int32(satellite), Epoch: epoch})
	if err != nil {
		return fleet.JournalCursor{}, mapError(err)
	}
	return fleet.JournalCursor{Epoch: row.Epoch, AckedSeq: row.AckedSeq, Fresh: row.Fresh}, nil
}

type edgeEventRow struct {
	Kind    fleet.EventKind `json:"kind"`
	Payload any             `json:"payload"`
}

func (r journalRepo) Advance(ctx context.Context, satellite fleet.SatelliteID, epoch, ackedSeq int64, events []fleet.EdgeEvent) error {
	edgeKinds := fleet.EdgeKinds()
	rows := make([]edgeEventRow, 0, len(events))
	for _, e := range events {
		if !slices.Contains(edgeKinds, e.Kind) {
			return fmt.Errorf("%w: %s is not an edge event", fleet.ErrInvalid, e.Kind)
		}
		rows = append(rows, edgeEventRow{Kind: e.Kind, Payload: e.Payload})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("%w: encode edge events: %w", fleet.ErrInvalid, err)
	}
	res, err := r.q.AdvanceJournalCursor(ctx, database.AdvanceJournalCursorParams{
		SatelliteID: int32(satellite), Epoch: epoch, AckedSeq: ackedSeq, Events: data,
	})
	if err != nil {
		return mapError(err)
	}
	if res.Moved == 0 {
		return fmt.Errorf("%w: journal cursor of satellite %d is not at epoch %d below %d", fleet.ErrConflict, satellite, epoch, ackedSeq)
	}
	return nil
}
