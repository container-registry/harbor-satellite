package postgres

import (
	"context"
	"database/sql"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ webhook.LogReader = (*OutboxLog)(nil)

// OutboxLog runs ReadOutboxAfter, whose xmin guard hides rows of
// transactions still in flight.
type OutboxLog struct{}

func NewOutboxLog(db *sql.DB) (*OutboxLog, error) {
	return nil, notimpl.Err("9.1 postgres outbox log")
}

func (l *OutboxLog) ReadAfter(ctx context.Context, after webhook.Position, limit int) ([]webhook.LogEntry, error) {
	return nil, notimpl.Err("9.1 postgres read outbox log")
}
