package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ async.DesiredStateStore = (*DesiredStateStore)(nil)

// DesiredStateStore runs the desired_state queries. The rows are derived:
// a group's artifacts live in group_artifacts, which only the API
// transaction writes.
type DesiredStateStore struct {
	q *database.Queries
}

func NewDesiredStateStore(db *sql.DB) (*DesiredStateStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the desired state store needs a database", fleet.ErrInvalid)
	}
	return &DesiredStateStore{q: database.New(db)}, nil
}

func (s *DesiredStateStore) Record(ctx context.Context, aggregate fleet.Aggregate, document json.RawMessage, digest fleet.Digest) (version int64, publish bool, err error) {
	row, err := s.q.RecordDesiredState(ctx, database.RecordDesiredStateParams{
		AggregateType: string(aggregate.Type), AggregateID: aggregate.ID, Document: document, RenderedDigest: string(digest),
	})
	if err != nil {
		return 0, false, mapError(err)
	}
	return row.Version, row.Publish, nil
}

func (s *DesiredStateStore) MarkPublished(ctx context.Context, aggregate fleet.Aggregate, rendered, artifact fleet.Digest) error {
	_, err := s.q.MarkDesiredStatePublished(ctx, database.MarkDesiredStatePublishedParams{
		AggregateType: string(aggregate.Type), AggregateID: aggregate.ID, Digest: string(rendered), ArtifactDigest: string(artifact),
	})
	return mapError(err)
}

func (s *DesiredStateStore) ClearPublished(ctx context.Context, aggregate fleet.Aggregate) error {
	return mapError(s.q.ClearDesiredStatePublished(ctx, database.ClearDesiredStatePublishedParams{
		AggregateType: string(aggregate.Type), AggregateID: aggregate.ID,
	}))
}

func (s *DesiredStateStore) MarkFailed(ctx context.Context, aggregate fleet.Aggregate, cause string) error {
	return mapError(s.q.MarkDesiredStateFailed(ctx, database.MarkDesiredStateFailedParams{
		AggregateType: string(aggregate.Type), AggregateID: aggregate.ID, LastError: cause,
	}))
}

// Get returns fleet.ErrNotFound for an aggregate without a row.
func (s *DesiredStateStore) Get(ctx context.Context, aggregate fleet.Aggregate) (async.DesiredRecord, error) {
	row, err := s.q.GetDesiredState(ctx, database.GetDesiredStateParams{AggregateType: string(aggregate.Type), AggregateID: aggregate.ID})
	if err != nil {
		return async.DesiredRecord{}, mapError(err)
	}
	return async.DesiredRecord{
		Aggregate: aggregate, Version: row.Version, Document: row.Document,
		RenderedDigest: fleet.Digest(row.RenderedDigest), PublishedDigest: fleet.Digest(row.PublishedDigest.String),
		PublishedArtifactDigest: fleet.Digest(row.PublishedArtifactDigest.String),
		PublishedAt:             row.PublishedAt.Time, LastError: row.LastError.String,
	}, nil
}

func (s *DesiredStateStore) Delete(ctx context.Context, aggregate fleet.Aggregate) error {
	return mapError(s.q.DeleteDesiredState(ctx, database.DeleteDesiredStateParams{AggregateType: string(aggregate.Type), AggregateID: aggregate.ID}))
}
