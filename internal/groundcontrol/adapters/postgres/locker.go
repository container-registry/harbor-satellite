package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/advisorylock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ async.AggregateLocker = (*AggregateLocker)(nil)

// AggregateLocker holds one dedicated *sql.Conn and takes
// TryAggregateLock(namespace, id) on it, with the namespace of the
// aggregate type from advisorylock. The connection carries no transaction,
// so holding a lock across a registry call keeps no snapshot open.
// PostgreSQL advisory locks are reentrant per session, so the locker also
// keeps the aggregates it holds and refuses a second holder in-process.
type AggregateLocker struct {
	mu   sync.Mutex
	conn *sql.Conn
	q    *database.Queries
	held map[fleet.Aggregate]bool
}

func NewAggregateLocker(ctx context.Context, db *sql.DB) (*AggregateLocker, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the aggregate locker needs a database", fleet.ErrInvalid)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open aggregate lock connection: %w", err)
	}
	return &AggregateLocker{conn: conn, q: database.New(conn), held: map[fleet.Aggregate]bool{}}, nil
}

func namespace(t fleet.AggregateType) (int32, error) {
	switch t {
	case fleet.AggregateSatellite:
		return advisorylock.SatelliteAggregates, nil
	case fleet.AggregateGroup:
		return advisorylock.GroupAggregates, nil
	case fleet.AggregateConfig:
		return advisorylock.ConfigAggregates, nil
	case fleet.AggregatePolicy, fleet.AggregateEdge:
		return 0, fmt.Errorf("%w: %s events are log-only and never locked", fleet.ErrInvalid, t)
	default:
		return 0, fmt.Errorf("%w: no lock namespace for aggregate type %q", fleet.ErrInvalid, t)
	}
}

func (l *AggregateLocker) TryLock(ctx context.Context, aggregate fleet.Aggregate) (unlock func(), ok bool, err error) {
	ns, err := namespace(aggregate.Type)
	if err != nil {
		return nil, false, err
	}
	if aggregate.ID < 0 || aggregate.ID > 1<<31-1 {
		return nil, false, fmt.Errorf("%w: aggregate id %d", fleet.ErrInvalid, aggregate.ID)
	}
	id := int32(aggregate.ID)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil, false, errors.New("aggregate locker closed")
	}
	if l.held[aggregate] {
		return nil, false, nil
	}
	acquired, err := l.q.TryAggregateLock(ctx, database.TryAggregateLockParams{Namespace: ns, ID: id})
	if err != nil {
		return nil, false, fmt.Errorf("try aggregate lock: %w", err)
	}
	if !acquired {
		return nil, false, nil
	}
	l.held[aggregate] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			delete(l.held, aggregate)
			if l.conn == nil {
				return
			}
			// A lock that cannot be released goes with the session.
			if _, err := l.q.AggregateUnlock(context.Background(), database.AggregateUnlockParams{Namespace: ns, ID: id}); err != nil {
				if discardErr := discard(l.conn); discardErr != nil {
					log.Printf("discard aggregate lock session after unlock error %v: %v", err, discardErr)
				}
				l.conn = nil
				l.held = map[fleet.Aggregate]bool{}
			}
		})
	}, true, nil
}

// Close unlocks every held aggregate and returns the connection to the
// pool, or discards the session when an unlock fails.
func (l *AggregateLocker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	held := l.held
	err := release(l.conn, func() error {
		var errs []error
		for aggregate := range held {
			ns, err := namespace(aggregate.Type)
			if err == nil {
				_, err = l.q.AggregateUnlock(context.Background(), database.AggregateUnlockParams{Namespace: ns, ID: int32(aggregate.ID)})
			}
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	})
	l.conn = nil
	l.held = map[fleet.Aggregate]bool{}
	return err
}

// release returns conn to the pool once unlock succeeded. sql.Conn.Close
// keeps a healthy session open in the pool with its session-level locks,
// so a session whose locks may survive is discarded instead.
func release(conn *sql.Conn, unlock func() error) error {
	if err := unlock(); err != nil {
		return errors.Join(err, discard(conn))
	}
	return conn.Close()
}

// discard closes conn's session rather than pooling it, which ends every
// lock it holds: database/sql drops a connection whose use reported
// driver.ErrBadConn.
func discard(conn *sql.Conn) error {
	err := conn.Raw(func(any) error { return driver.ErrBadConn })
	if !errors.Is(err, driver.ErrBadConn) {
		return errors.Join(err, conn.Close())
	}
	return nil
}
