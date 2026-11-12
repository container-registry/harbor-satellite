package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"

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
	mu   sync.Mutex
	conn *sql.Conn
	q    *database.Queries
	held map[fleet.WebhookID]bool
}

func NewSubscriptionLocker(ctx context.Context, db *sql.DB) (*SubscriptionLocker, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the subscription locker needs a database", fleet.ErrInvalid)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open subscription lock connection: %w", err)
	}
	return &SubscriptionLocker{conn: conn, q: database.New(conn), held: map[fleet.WebhookID]bool{}}, nil
}

func (l *SubscriptionLocker) TryLock(ctx context.Context, id fleet.WebhookID) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil, false, errors.New("subscription locker closed")
	}
	if l.held[id] {
		return nil, false, nil
	}
	params := database.TryAggregateLockParams{Namespace: advisorylock.WebhookSubscriptions, ID: int32(id)}
	acquired, err := l.q.TryAggregateLock(ctx, params)
	if err != nil {
		return nil, false, fmt.Errorf("try subscription lock: %w", err)
	}
	if !acquired {
		return nil, false, nil
	}
	l.held[id] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			delete(l.held, id)
			if l.conn == nil {
				return
			}
			// A lock that cannot be released goes with the session.
			if _, err := l.q.AggregateUnlock(context.Background(), database.AggregateUnlockParams(params)); err != nil {
				if discardErr := discard(l.conn); discardErr != nil {
					log.Printf("discard subscription lock session after unlock error %v: %v", err, discardErr)
				}
				l.conn = nil
				l.held = map[fleet.WebhookID]bool{}
			}
		})
	}, true, nil
}

// Close unlocks every held subscription and returns the connection to the
// pool, or discards the session when an unlock fails.
func (l *SubscriptionLocker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	held := l.held
	err := release(l.conn, func() error {
		var errs []error
		for id := range held {
			_, err := l.q.AggregateUnlock(context.Background(), database.AggregateUnlockParams{Namespace: advisorylock.WebhookSubscriptions, ID: int32(id)})
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	})
	l.conn = nil
	l.held = map[fleet.WebhookID]bool{}
	return err
}
