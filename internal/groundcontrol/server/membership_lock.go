package server

import (
	"context"
	"database/sql"
)

// membershipLockID keys the transaction-scoped advisory lock taken by every
// handler that rewrites robot projects or satellite state from group
// membership. It differs from cleanupLockID.
const membershipLockID = 12346

// lockGroupMembership waits until no other transaction is changing group
// membership or group projects. Each of those handlers reads the satellite's
// groups and then writes the result to Harbor, so two of them running at once
// can each miss the other's change and the last Harbor write wins. Postgres
// releases the lock when tx commits or rolls back. Call it before any other
// statement in tx so the lock order is always the same.
func lockGroupMembership(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", membershipLockID)
	return err
}
