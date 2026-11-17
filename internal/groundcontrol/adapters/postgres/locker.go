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
	session *lockSession[fleet.Aggregate]
}

func NewAggregateLocker(ctx context.Context, db *sql.DB) (*AggregateLocker, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the aggregate locker needs a database", fleet.ErrInvalid)
	}
	session, err := openLockSession[fleet.Aggregate](ctx, db, "aggregate")
	if err != nil {
		return nil, err
	}
	return &AggregateLocker{session: session}, nil
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
	return l.session.tryLock(ctx, aggregate, database.TryAggregateLockParams{Namespace: ns, ID: int32(aggregate.ID)})
}

// Close unlocks every held aggregate and returns the connection to the
// pool, or discards the session when an unlock fails.
func (l *AggregateLocker) Close() error { return l.session.close() }

// lockSession takes session-level advisory locks on one dedicated
// connection. When that session is lost, it ends every lock taken on it,
// and the next tryLock opens a new one from the pool.
type lockSession[K comparable] struct {
	name   string
	db     *sql.DB
	mu     sync.Mutex
	conn   *sql.Conn
	q      *database.Queries
	held   map[K]database.TryAggregateLockParams
	closed bool
}

func openLockSession[K comparable](ctx context.Context, db *sql.DB, name string) (*lockSession[K], error) {
	s := &lockSession[K]{name: name, db: db, held: map[K]database.TryAggregateLockParams{}}
	if err := s.open(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *lockSession[K]) open(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open %s lock connection: %w", s.name, err)
	}
	s.conn, s.q = conn, database.New(conn)
	return nil
}

// tryLock never lets a context cancel a query on the session: lib/pq marks a
// connection bad when its context ends mid-call, database/sql then closes it,
// and that ends every lock held on the session while its holders still run.
// The pool's server-side statement_timeout bounds these queries instead.
func (s *lockSession[K]) tryLock(ctx context.Context, key K, params database.TryAggregateLockParams) (func(), bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("try %s lock: %w", s.name, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false, fmt.Errorf("%s locker closed", s.name)
	}
	if s.conn == nil {
		if err := s.open(ctx); err != nil {
			return nil, false, err
		}
	}
	if _, ok := s.held[key]; ok {
		return nil, false, nil
	}
	session := context.WithoutCancel(ctx)
	acquired, err := s.q.TryAggregateLock(session, params)
	if err != nil {
		// A statement timeout leaves the session and its locks intact; a
		// session that no longer answers a ping has lost them.
		if pingErr := s.conn.PingContext(session); pingErr != nil {
			s.drop()
		}
		return nil, false, fmt.Errorf("try %s lock: %w", s.name, err)
	}
	if !acquired {
		return nil, false, nil
	}
	s.held[key] = params
	conn := s.conn
	var once sync.Once
	return func() { once.Do(func() { s.unlock(conn, key, params) }) }, true, nil
}

func (s *lockSession[K]) unlock(conn *sql.Conn, key K, params database.TryAggregateLockParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The lock ended with the session it was taken on; the key may now be
	// held by someone else on the current session.
	if s.conn != conn {
		return
	}
	delete(s.held, key)
	if _, err := s.q.AggregateUnlock(context.Background(), database.AggregateUnlockParams(params)); err != nil {
		log.Printf("unlock %s lock (%d, %d): %v; discarding the session", s.name, params.Namespace, params.ID, err)
		s.drop()
	}
}

// drop discards the session, which ends every lock it holds.
func (s *lockSession[K]) drop() {
	if err := discard(s.conn); err != nil {
		log.Printf("discard %s lock session: %v", s.name, err)
	}
	s.conn, s.q = nil, nil
	s.held = map[K]database.TryAggregateLockParams{}
}

func (s *lockSession[K]) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn == nil {
		return nil
	}
	held := s.held
	err := release(s.conn, func() error {
		var errs []error
		for _, params := range held {
			_, err := s.q.AggregateUnlock(context.Background(), database.AggregateUnlockParams(params))
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	})
	s.conn, s.q = nil, nil
	s.held = map[K]database.TryAggregateLockParams{}
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
// driver.ErrBadConn. A conn that database/sql already closed has no
// session left.
func discard(conn *sql.Conn) error {
	err := conn.Raw(func(any) error { return driver.ErrBadConn })
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return nil
	}
	return errors.Join(err, conn.Close())
}
