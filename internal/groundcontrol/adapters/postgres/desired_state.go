package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ async.DesiredStateStore = (*DesiredStateStore)(nil)

// DesiredStateStore runs the desired_state queries. The rows are derived:
// a group's artifacts live in group_artifacts, which only the API
// transaction writes.
type DesiredStateStore struct{}

func NewDesiredStateStore(db *sql.DB) (*DesiredStateStore, error) {
	return nil, notimpl.Err("2.1 desired state store")
}

func (s *DesiredStateStore) Record(ctx context.Context, aggregate fleet.Aggregate, document json.RawMessage, digest fleet.Digest) (version int64, publish bool, err error) {
	return 0, false, notimpl.Err("2.1 record desired state")
}

func (s *DesiredStateStore) MarkPublished(ctx context.Context, aggregate fleet.Aggregate, rendered, artifact fleet.Digest) error {
	return notimpl.Err("2.1 mark desired state published")
}

func (s *DesiredStateStore) ClearPublished(ctx context.Context, aggregate fleet.Aggregate) error {
	return notimpl.Err("2.1 clear desired state published")
}

func (s *DesiredStateStore) MarkFailed(ctx context.Context, aggregate fleet.Aggregate, cause string) error {
	return notimpl.Err("2.1 mark desired state failed")
}

// Get returns fleet.ErrNotFound for an aggregate without a row.
func (s *DesiredStateStore) Get(ctx context.Context, aggregate fleet.Aggregate) (async.DesiredRecord, error) {
	return async.DesiredRecord{}, notimpl.Err("2.1 get desired state")
}

func (s *DesiredStateStore) Delete(ctx context.Context, aggregate fleet.Aggregate) error {
	return notimpl.Err("2.1 delete desired state")
}
