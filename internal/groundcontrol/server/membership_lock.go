package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
)

// lockGroupMembership takes the transaction-scoped lock held by every handler
// that writes robot projects or satellite state derived from group membership.
// Call it before any other statement in tx, so concurrent handlers read
// memberships only after the previous one has committed. That holds under the
// default READ COMMITTED isolation; a REPEATABLE READ tx would keep the
// snapshot taken before the wait. Postgres releases the lock when tx ends.
func lockGroupMembership(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", membershipLockID); err != nil {
		log.Printf("Error taking group membership lock: %v", err)
		return &AppError{
			Message: "Error: Failed to lock group membership",
			Code:    http.StatusInternalServerError,
		}
	}
	return nil
}
