package async

import (
	"context"
	"encoding/json"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// OutboxEvent is a claimed outbox row. Attempts counts claims, this one
// included.
type OutboxEvent struct {
	ID                       int64
	Kind                     fleet.EventKind
	Aggregate                fleet.Aggregate
	Payload                  json.RawMessage
	Attempts                 int32
	CreatedAt, NextAttemptAt time.Time
	LastError                string
}

// OutboxStore is the work-queue view of the outbox.
type OutboxStore interface {
	// Claim leases up to batchSize due events below maxAttempts for lease
	// and counts the claim as an attempt. Durations, not times, cross the
	// port: the store computes every time on the database's clock.
	Claim(ctx context.Context, batchSize, maxAttempts int, lease time.Duration) ([]OutboxEvent, error)
	Complete(ctx context.Context, ids []int64) error
	// Fail records cause and makes the event due again after retryIn.
	Fail(ctx context.Context, id int64, cause string, retryIn time.Duration) error
	// Release gives the claim's attempt back, for a failure that is not
	// the event's own: the registry was unreachable or another worker
	// holds the aggregate.
	Release(ctx context.Context, id int64, cause string, retryIn time.Duration) error
	// Rearm makes every event that ran out of maxAttempts due again and
	// returns how many.
	Rearm(ctx context.Context, maxAttempts int) (int64, error)
	// Stuck lists events that are not done and were claimed at least
	// minAttempts times, oldest first.
	Stuck(ctx context.Context, minAttempts, limit int) ([]OutboxEvent, error)
	PendingCount(ctx context.Context) (int64, error)
	// Prune deletes up to limit done events older than retention that no
	// enabled webhook checkpoint still needs (plan item 9.1). It runs once
	// across workers under advisorylock.OutboxPruner and returns 0 while
	// another worker holds it.
	Prune(ctx context.Context, retention time.Duration, limit int) (int64, error)
}

// DesiredRecord is one row of the desired state: what GC last rendered for
// an aggregate and what it published. It is derived from the fleet tables
// and can be rebuilt from them.
type DesiredRecord struct {
	Aggregate                       fleet.Aggregate
	Version                         int64
	Document                        json.RawMessage
	RenderedDigest, PublishedDigest fleet.Digest
	// PublishedArtifactDigest is the manifest digest the registry returned;
	// satellites report it back as the state they applied.
	PublishedArtifactDigest fleet.Digest
	PublishedAt             time.Time
	LastError               string
}

// DesiredStateStore holds one desired-state row per aggregate.
type DesiredStateStore interface {
	// Record stores the rendered document. version moves only when digest
	// changed; publish is true while digest is not the published one.
	Record(ctx context.Context, aggregate fleet.Aggregate, document json.RawMessage, digest fleet.Digest) (version int64, publish bool, err error)
	// MarkPublished records rendered and the registry's artifact digest
	// as published if rendered is still the rendered digest.
	MarkPublished(ctx context.Context, aggregate fleet.Aggregate, rendered, artifact fleet.Digest) error
	MarkFailed(ctx context.Context, aggregate fleet.Aggregate, cause string) error
	// ClearPublished makes the next reconcile publish again.
	ClearPublished(ctx context.Context, aggregate fleet.Aggregate) error
	// Get returns fleet.ErrNotFound for an aggregate without a row.
	Get(ctx context.Context, aggregate fleet.Aggregate) (DesiredRecord, error)
	Delete(ctx context.Context, aggregate fleet.Aggregate) error
}

// AggregateLocker serialises work on one aggregate across workers. The
// lock is session-level on a dedicated connection, so no transaction stays
// open across a registry call.
type AggregateLocker interface {
	// TryLock does not wait: ok is false while another worker holds the
	// aggregate. unlock releases the lock and never fails; on a broken
	// connection the lock went with the session.
	TryLock(ctx context.Context, aggregate fleet.Aggregate) (unlock func(), ok bool, err error)
}

// Reachability tells the publisher whether to claim at all.
type Reachability interface {
	Reachable() bool
}
