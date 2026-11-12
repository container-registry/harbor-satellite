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
// fleet.ErrAlreadyExists and foreign_key_violation (23503) fleet.ErrInUse.
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
		case "23503":
			return fmt.Errorf("%w: %w", fleet.ErrInUse, err)
		}
	}
	return err
}
