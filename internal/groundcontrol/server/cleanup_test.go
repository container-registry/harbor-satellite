package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/stretchr/testify/require"
)

// advisoryLockBackend models Postgres session-level advisory locks: a lock
// belongs to the connection that took it and is dropped when that
// connection closes.
type advisoryLockBackend struct {
	mu            sync.Mutex
	nextConnID    int
	owners        map[int64]int
	failedUnlocks int
	onDelete      func()
}

func (b *advisoryLockBackend) Connect(context.Context) (driver.Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextConnID++
	return &advisoryLockConn{backend: b, id: b.nextConnID}, nil
}

func (b *advisoryLockBackend) Driver() driver.Driver { return b }

func (b *advisoryLockBackend) Open(string) (driver.Conn, error) {
	return nil, errors.New("use the connector")
}

type advisoryLockConn struct {
	backend *advisoryLockBackend
	id      int
}

func (c *advisoryLockConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported")
}

func (c *advisoryLockConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (c *advisoryLockConn) Close() error {
	c.backend.mu.Lock()
	defer c.backend.mu.Unlock()
	for lockID, owner := range c.backend.owners {
		if owner == c.id {
			delete(c.backend.owners, lockID)
		}
	}
	return nil
}

func (c *advisoryLockConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	lockID, ok := args[0].Value.(int64)
	if !ok {
		return nil, errors.New("lock id must be an integer")
	}

	b := c.backend
	b.mu.Lock()
	defer b.mu.Unlock()

	owner, held := b.owners[lockID]
	switch {
	case strings.Contains(query, "pg_try_advisory_lock"):
		if held && owner != c.id {
			return &boolRows{value: false}, nil
		}
		b.owners[lockID] = c.id
		return &boolRows{value: true}, nil
	case strings.Contains(query, "pg_advisory_unlock"):
		if !held || owner != c.id {
			b.failedUnlocks++
			return &boolRows{value: false}, nil
		}
		delete(b.owners, lockID)
		return &boolRows{value: true}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}

func (c *advisoryLockConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "DELETE") {
		if _, err := c.QueryContext(ctx, query, args); err != nil {
			return nil, err
		}
		return driver.RowsAffected(0), nil
	}
	if c.backend.onDelete != nil {
		c.backend.onDelete()
	}
	return driver.RowsAffected(0), nil
}

type boolRows struct {
	value bool
	done  bool
}

func (r *boolRows) Columns() []string { return []string{"result"} }

func (r *boolRows) Close() error { return nil }

func (r *boolRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}

func TestRunCleanupWithLockKeepsLockOnOneSession(t *testing.T) {
	ctx := context.Background()
	backend := &advisoryLockBackend{owners: map[int64]int{}}
	db := sql.OpenDB(backend)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	db.SetMaxIdleConns(0)

	var otherSessionAcquired []bool
	backend.onDelete = func() {
		conn, err := db.Conn(ctx)
		require.NoError(t, err)
		defer func() { require.NoError(t, conn.Close()) }()

		var acquired bool
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", cleanupLockID).Scan(&acquired))
		otherSessionAcquired = append(otherSessionAcquired, acquired)
	}

	s := &Server{db: db, dbQueries: database.New(db)}
	s.runCleanupWithLock(ctx, defaultRetentionDays)

	require.Equal(t, []bool{false, false}, otherSessionAcquired, "another instance took the cleanup lock while cleanup was running")
	require.Zero(t, backend.failedUnlocks, "the lock was released from a session that did not hold it")
	require.Empty(t, backend.owners)
}
