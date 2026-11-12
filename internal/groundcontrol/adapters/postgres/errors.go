package postgres

import (
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// mapError maps driver errors onto the fleet sentinels, keeping the cause:
// sql.ErrNoRows is fleet.ErrNotFound, unique_violation (23505)
// fleet.ErrAlreadyExists and foreign_key_violation (23503) fleet.ErrInUse.
// Other errors pass through.
func mapError(err error) error {
	return fmt.Errorf("%w: 1.3 postgres error mapping of %w", notimpl.ErrNotImplemented, err)
}
