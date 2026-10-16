package webhook

import (
	"context"
	"encoding/json"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// Position is an outbox row's place in the log: its transaction ID, then
// its id.
type Position struct {
	XID uint64
	ID  int64
}

type LogEntry struct {
	Position  Position
	Kind      fleet.EventKind
	Aggregate fleet.Aggregate
	Payload   json.RawMessage
	CreatedAt time.Time
}

type DueSubscription struct {
	ID           fleet.WebhookID
	URL          string
	SealedSecret []byte
	EventTypes   []fleet.EventKind
	After        Position
	Attempts     int32
}

// LogReader reads the outbox after a position; rows of transactions still
// in flight stay hidden until they commit, so a checkpoint never passes one.
type LogReader interface {
	ReadAfter(ctx context.Context, after Position, limit int) ([]LogEntry, error)
}

type SubscriptionStore interface {
	Due(ctx context.Context, limit int) ([]DueSubscription, error)
	Advance(ctx context.Context, id fleet.WebhookID, to Position, status int) error
	// Fail disables the subscription once it has failed without a delivery
	// for disableAfter.
	Fail(ctx context.Context, id fleet.WebhookID, status int, cause string, retryIn, disableAfter time.Duration) (disabled bool, err error)
}

// Locker serialises one subscription across workers with a session-level
// two-key lock (namespace advisorylock.WebhookSubscriptions, the INT id as
// second key); no transaction stays open across a delivery.
type Locker interface {
	TryLock(ctx context.Context, id fleet.WebhookID) (unlock func(), ok bool, err error)
}

type Sender interface {
	Send(ctx context.Context, url string, secret []byte, event CloudEvent) (status int, err error)
}
