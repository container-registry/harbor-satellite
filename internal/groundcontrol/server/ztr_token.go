package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var errTokenConsumed = errors.New("registration token already consumed")

// claimZTRToken claims token in one statement, so exactly one of several
// concurrent registrations wins. A missing token is fleet.ErrTokenInvalid and
// an already claimed one fleet.ErrTokenInvalid wrapping errTokenConsumed; Ztr
// answers both like an unknown token so the response does not reveal that the
// token is in use. An expired token is fleet.ErrTokenExpired.
func claimZTRToken(_ context.Context, _ *database.Queries, token string) (int32, error) {
	return 0, fmt.Errorf("%w: D7 claim token %s", notimpl.ErrNotImplemented, maskToken(token))
}

// unclaimZTRToken undoes a claim after a later registration step failed, so
// the satellite can retry with the same token.
func unclaimZTRToken(_ context.Context, _ *database.Queries, token string) error {
	return fmt.Errorf("%w: D7 unclaim token %s", notimpl.ErrNotImplemented, maskToken(token))
}
