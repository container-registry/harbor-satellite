package postgres

import (
	"context"
	"database/sql"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ webhook.Locker = (*SubscriptionLocker)(nil)

// SubscriptionLocker takes the session-level two-key lock
// (advisorylock.WebhookSubscriptions, subscription id) on a dedicated
// connection, so no transaction stays open across a delivery.
type SubscriptionLocker struct{}

func NewSubscriptionLocker(ctx context.Context, db *sql.DB) (*SubscriptionLocker, error) {
	return nil, notimpl.Err("9.1 postgres subscription locker")
}

func (l *SubscriptionLocker) TryLock(ctx context.Context, id fleet.WebhookID) (func(), bool, error) {
	return nil, false, notimpl.Err("9.1 postgres lock subscription")
}

// Close releases every lock with the connection.
func (l *SubscriptionLocker) Close() error {
	return notimpl.Err("9.1 postgres subscription locker close")
}
