package postgres

import (
	"context"
	"database/sql"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ heartbeat.Store = (*HeartbeatStore)(nil)

// HeartbeatStore writes a heartbeat batch in one transaction on the fleet
// plane's pool: FlushLastSeen, FlushSatelliteView,
// InsertSatelliteStatusBatch and, when the batch has any,
// StoreSatelliteInventories and AckRobotRotations, each with the whole batch as one JSON
// argument. The statements read times as TIMESTAMPTZ, so
// satellites.last_seen and satellite_status.reported_at, which are
// TIMESTAMP, get the session time zone's wall clock, as NOW() writes them.
type HeartbeatStore struct{}

func NewHeartbeatStore(db *sql.DB, clock fleet.Clock) (*HeartbeatStore, error) {
	return nil, notimpl.Err("4.2 postgres heartbeat store")
}

func (s *HeartbeatStore) Flush(ctx context.Context, batch heartbeat.Batch) error {
	return notimpl.Err("4.2 postgres heartbeat flush")
}
