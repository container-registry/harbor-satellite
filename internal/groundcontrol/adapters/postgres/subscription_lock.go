package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
)

var _ webhook.Locker = (*SubscriptionLocker)(nil)

// SubscriptionLocker takes the session-level two-key lock
// (advisorylock.WebhookSubscriptions, subscription id) on a dedicated
// connection, so no transaction stays open across a delivery.
type SubscriptionLocker struct {
	session *lockSession[fleet.WebhookID]
}

func NewSubscriptionLocker(ctx context.Context, db *sql.DB) (*SubscriptionLocker, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the subscription locker needs a database", fleet.ErrInvalid)
	}
	session, err := openLockSession[fleet.WebhookID](ctx, db, "subscription")
	if err != nil {
		return nil, err
	}
	return &SubscriptionLocker{session: session}, nil
}

func (l *SubscriptionLocker) TryLock(ctx context.Context, id fleet.WebhookID) (func(), bool, error) {
	return l.session.tryLock(ctx, id, database.TryAggregateLockParams{Namespace: advisorylock.WebhookSubscriptions, ID: int32(id)})
}

// Close unlocks every held subscription and returns the connection to the
// pool, or discards the session when an unlock fails.
func (l *SubscriptionLocker) Close() error { return l.session.close() }
