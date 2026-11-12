package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var (
	_ async.OutboxStore = (*OutboxStore)(nil)
	_ fleet.EventSink   = (*OutboxSink)(nil)
)

// OutboxStore runs ClaimOutboxEvents, CompleteOutboxEvents,
// FailOutboxEvent, ListStuckOutboxEvents and CountPendingOutboxEvents on
// the async plane's pool.
type OutboxStore struct {
	db *sql.DB
	q  *database.Queries
}

func NewOutboxStore(db *sql.DB) (*OutboxStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the outbox store needs a database", fleet.ErrInvalid)
	}
	return &OutboxStore{db: db, q: database.New(db)}, nil
}

func millis(d time.Duration) int64 { return d.Milliseconds() }

// Claim returns the events ordered by ID.
func (s *OutboxStore) Claim(ctx context.Context, batchSize, maxAttempts int, lease time.Duration) ([]async.OutboxEvent, error) {
	rows, err := s.q.ClaimOutboxEvents(ctx, database.ClaimOutboxEventsParams{
		LeaseMs: millis(lease), MaxAttempts: int32(min(maxAttempts, 1<<30)), BatchSize: int32(min(batchSize, 1<<30)),
	})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]async.OutboxEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, async.OutboxEvent{
			ID: r.ID, Kind: fleet.EventKind(r.Kind), Payload: r.Payload, Attempts: r.Attempts, CreatedAt: r.CreatedAt,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateType(r.AggregateType), ID: r.AggregateID},
		})
	}
	// The UPDATE ... RETURNING of the claim does not keep the CTE's order.
	sortByID(out)
	return out, nil
}

func sortByID(events []async.OutboxEvent) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].ID < events[j-1].ID; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}

func (s *OutboxStore) Complete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return mapError(s.q.CompleteOutboxEvents(ctx, ids))
}

func (s *OutboxStore) Fail(ctx context.Context, id int64, cause string, retryIn time.Duration) error {
	return mapError(s.q.FailOutboxEvent(ctx, database.FailOutboxEventParams{ID: id, LastError: cause, RetryInMs: millis(retryIn)}))
}

func (s *OutboxStore) Release(ctx context.Context, id int64, cause string, retryIn time.Duration) error {
	return mapError(s.q.ReleaseOutboxEvent(ctx, database.ReleaseOutboxEventParams{ID: id, LastError: cause, RetryInMs: millis(retryIn)}))
}

func (s *OutboxStore) Rearm(ctx context.Context, maxAttempts int) (int64, error) {
	n, err := s.q.RearmOutboxEvents(ctx, int32(min(maxAttempts, 1<<30)))
	return n, mapError(err)
}

func (s *OutboxStore) Stuck(ctx context.Context, minAttempts, limit int) ([]async.OutboxEvent, error) {
	rows, err := s.q.ListStuckOutboxEvents(ctx, database.ListStuckOutboxEventsParams{
		MinAttempts: int32(min(minAttempts, 1<<30)), MaxRows: int32(min(limit, 1<<30)),
	})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]async.OutboxEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, async.OutboxEvent{
			ID: r.ID, Kind: fleet.EventKind(r.Kind), Attempts: r.Attempts, CreatedAt: r.CreatedAt,
			NextAttemptAt: r.NextAttemptAt, LastError: r.LastError.String,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateType(r.AggregateType), ID: r.AggregateID},
		})
	}
	return out, nil
}

func (s *OutboxStore) PendingCount(ctx context.Context) (int64, error) {
	n, err := s.q.CountPendingOutboxEvents(ctx)
	return n, mapError(err)
}

// Prune takes advisorylock.OutboxPruner with TryAdvisoryXactLock and runs
// PruneDoneOutboxEvents in one transaction.
func (s *OutboxStore) Prune(ctx context.Context, retention time.Duration, limit int) (int64, error) {
	return 0, notimpl.Err("9.1 outbox prune")
}

// OutboxSink appends events with AppendOutboxEvent inside the caller's
// transaction, so an event exists exactly when its change committed. The
// payload is encoded as JSON; log-only kinds are inserted done.
type OutboxSink struct {
	q *database.Queries
}

func NewOutboxSink(q *database.Queries) (*OutboxSink, error) {
	if q == nil {
		return nil, fmt.Errorf("%w: the outbox sink needs queries", fleet.ErrInvalid)
	}
	return &OutboxSink{q: q}, nil
}

func (s *OutboxSink) Append(ctx context.Context, events ...fleet.Event) error {
	for _, e := range events {
		payload, err := json.Marshal(e.Payload)
		if err != nil {
			return fmt.Errorf("encode %s payload: %w", e.Kind, err)
		}
		if e.Payload == nil {
			payload = json.RawMessage(`{}`)
		}
		_, err = s.q.AppendOutboxEvent(ctx, database.AppendOutboxEventParams{
			Kind: string(e.Kind), AggregateType: string(e.Aggregate.Type), AggregateID: e.Aggregate.ID,
			Payload: payload, LogOnly: e.Kind.LogOnly(),
		})
		if err != nil {
			return fmt.Errorf("append %s event: %w", e.Kind, mapError(err))
		}
	}
	return nil
}

// AppendToGroupMembers runs AppendGroupMemberEvents.
func (s *OutboxSink) AppendToGroupMembers(ctx context.Context, group fleet.GroupID, kind fleet.EventKind) error {
	_, err := s.q.AppendGroupMemberEvents(ctx, database.AppendGroupMemberEventsParams{Kind: string(kind), GroupID: int32(group)})
	return mapError(err)
}
