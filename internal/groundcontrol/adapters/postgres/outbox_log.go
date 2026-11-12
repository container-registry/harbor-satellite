package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
)

var _ webhook.LogReader = (*OutboxLog)(nil)

// OutboxLog runs ReadOutboxAfter, whose xmin guard hides rows of
// transactions still in flight.
type OutboxLog struct{ q *database.Queries }

func NewOutboxLog(db *sql.DB) (*OutboxLog, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the outbox log needs a database", fleet.ErrInvalid)
	}
	return &OutboxLog{q: database.New(db)}, nil
}

func (l *OutboxLog) ReadAfter(ctx context.Context, after webhook.Position, limit int) ([]webhook.LogEntry, error) {
	rows, err := l.q.ReadOutboxAfter(ctx, database.ReadOutboxAfterParams{
		AfterXid: strconv.FormatUint(after.XID, 10), AfterID: after.ID, MaxRows: int32(limit),
	})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]webhook.LogEntry, 0, len(rows))
	for _, r := range rows {
		xid, err := strconv.ParseUint(r.Xid, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("outbox event %d: xid %q: %w", r.ID, r.Xid, err)
		}
		out = append(out, webhook.LogEntry{
			Position: webhook.Position{XID: xid, ID: r.ID}, Kind: fleet.EventKind(r.Kind),
			Aggregate: fleet.Aggregate{Type: fleet.AggregateType(r.AggregateType), ID: r.AggregateID},
			Payload:   r.Payload, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
