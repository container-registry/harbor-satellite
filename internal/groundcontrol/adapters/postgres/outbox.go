package postgres

import (
	"context"
	"database/sql"
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
type OutboxStore struct{}

func NewOutboxStore(db *sql.DB) (*OutboxStore, error) {
	return nil, notimpl.Err("2.1 outbox store")
}

// Claim returns the events ordered by ID.
func (s *OutboxStore) Claim(ctx context.Context, batchSize, maxAttempts int, lease time.Duration) ([]async.OutboxEvent, error) {
	return nil, notimpl.Err("2.1 outbox claim")
}

func (s *OutboxStore) Complete(ctx context.Context, ids []int64) error {
	return notimpl.Err("2.1 outbox complete")
}

func (s *OutboxStore) Fail(ctx context.Context, id int64, cause string, retryIn time.Duration) error {
	return notimpl.Err("2.1 outbox fail")
}

func (s *OutboxStore) Release(ctx context.Context, id int64, cause string, retryIn time.Duration) error {
	return notimpl.Err("2.1 outbox release")
}

func (s *OutboxStore) Rearm(ctx context.Context, maxAttempts int) (int64, error) {
	return 0, notimpl.Err("2.1 outbox rearm")
}

func (s *OutboxStore) Stuck(ctx context.Context, minAttempts, limit int) ([]async.OutboxEvent, error) {
	return nil, notimpl.Err("2.1 outbox stuck")
}

func (s *OutboxStore) PendingCount(ctx context.Context) (int64, error) {
	return 0, notimpl.Err("2.1 outbox pending count")
}

// Prune takes advisorylock.OutboxPruner with TryAdvisoryXactLock and runs
// PruneDoneOutboxEvents in one transaction.
func (s *OutboxStore) Prune(ctx context.Context, retention time.Duration, limit int) (int64, error) {
	return 0, notimpl.Err("9.1 outbox prune")
}

// OutboxSink appends events with AppendOutboxEvent inside the caller's
// transaction, so an event exists exactly when its change committed. The
// payload is encoded as JSON; log-only kinds are inserted done.
type OutboxSink struct{}

func NewOutboxSink(q *database.Queries) (*OutboxSink, error) {
	return nil, notimpl.Err("2.1 outbox sink")
}

func (s *OutboxSink) Append(ctx context.Context, events ...fleet.Event) error {
	return notimpl.Err("2.1 outbox append")
}

// AppendToGroupMembers runs AppendGroupMemberEvents.
func (s *OutboxSink) AppendToGroupMembers(ctx context.Context, group fleet.GroupID, kind fleet.EventKind) error {
	return notimpl.Err("2.1 outbox member events")
}
