package postgres

import (
	"context"
	"database/sql"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ async.Job = (*PartitionMaintainer)(nil)

// PartitionMaintainer owns satellite_status retention once
// sql/pending/satellite_status_partitions.sql is a migration (plan item
// 4.5); the worker runs it.
type PartitionMaintainer struct{}

func NewPartitionMaintainer(db *sql.DB, clock fleet.Clock, retentionDays, daysAhead int) (*PartitionMaintainer, error) {
	return nil, notimpl.Err("4.5 partition maintainer")
}

func (m *PartitionMaintainer) Name() string { return "status-partitions" }

// Run calls RunOnce daily until ctx ends.
func (m *PartitionMaintainer) Run(ctx context.Context) error {
	return notimpl.Err("4.5 partition maintainer run")
}

// RunOnce takes advisorylock.StatusPartitions, creates satellite_status_YYYYMMDD for today and daysAhead, drops partitions older than retentionDays; a no-op while satellite_status is not partitioned.
func (m *PartitionMaintainer) RunOnce(ctx context.Context) error {
	return notimpl.Err("4.5 partition maintainer run once")
}
