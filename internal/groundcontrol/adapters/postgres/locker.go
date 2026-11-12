package postgres

import (
	"context"
	"database/sql"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ async.AggregateLocker = (*AggregateLocker)(nil)

// AggregateLocker holds one dedicated *sql.Conn and takes
// TryAggregateLock(namespace, id) on it, with the namespace of the
// aggregate type from advisorylock. The connection carries no transaction,
// so holding a lock across a registry call keeps no snapshot open.
type AggregateLocker struct{}

func NewAggregateLocker(ctx context.Context, db *sql.DB) (*AggregateLocker, error) {
	return nil, notimpl.Err("2.3 aggregate locker")
}

func (l *AggregateLocker) TryLock(ctx context.Context, aggregate fleet.Aggregate) (unlock func(), ok bool, err error) {
	return nil, false, notimpl.Err("2.3 aggregate lock")
}

// Close releases every lock with the connection.
func (l *AggregateLocker) Close() error {
	return notimpl.Err("2.3 aggregate locker close")
}
