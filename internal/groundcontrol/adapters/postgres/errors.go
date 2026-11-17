package postgres

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/lib/pq"
)

// mapError maps driver errors onto the fleet sentinels, keeping the cause:
// sql.ErrNoRows is fleet.ErrNotFound, unique_violation (23505)
// fleet.ErrAlreadyExists, and foreign_key_violation (23503) and
// restrict_violation (23001, what PostgreSQL 19 reports for an ON DELETE
// RESTRICT key) fleet.ErrInUse.
// Other errors pass through.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %w", fleet.ErrNotFound, err)
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23505":
			return fmt.Errorf("%w: %w", fleet.ErrAlreadyExists, err)
		case "23503", "23001":
			return fmt.Errorf("%w: %w", fleet.ErrInUse, err)
		}
	}
	return err
}

// rowLimit narrows a batch size to an SQL INT LIMIT. Sizes come from the
// environment, so one above the INT range is capped instead of wrapping
// into a negative LIMIT.
func rowLimit(n int) int32 {
	return int32(min(n, 1<<30))
}
