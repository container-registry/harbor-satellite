package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// tokenRepo runs D7's ClaimToken, UnclaimToken and ConsumeToken. ClaimToken
// returns no row for a missing, claimed and expired token alike, so Claim
// reads the token with GetTokenByValue to tell them apart: missing is
// ErrTokenInvalid, claimed fmt.Errorf("%w: already consumed",
// ErrTokenInvalid) so the audit log keeps the reason while the response
// stays 400, expired ErrTokenExpired. A token neither consumed nor expired
// was claimed and unclaimed by a concurrent registration between the two
// statements, so it is ErrTokenInvalid too. This is the only place that
// classifies tokens once plan item 1.4 deletes server.claimZTRToken.
type tokenRepo struct{ q *database.Queries }

func (r tokenRepo) Add(ctx context.Context, satellite fleet.SatelliteID, token string, expiresAt time.Time) error {
	_, err := r.q.AddToken(ctx, database.AddTokenParams{SatelliteID: int32(satellite), Token: token, ExpiresAt: expiresAt})
	return mapError(err)
}

func (r tokenRepo) Claim(ctx context.Context, token string, now time.Time) (fleet.SatelliteID, error) {
	id, err := r.q.ClaimToken(ctx, database.ClaimTokenParams{Token: token, Now: now})
	if err == nil {
		return fleet.SatelliteID(id), nil
	}
	if !errors.Is(mapError(err), fleet.ErrNotFound) {
		return 0, mapError(err)
	}
	row, err := r.q.GetTokenByValue(ctx, token)
	switch {
	case errors.Is(mapError(err), fleet.ErrNotFound):
		return 0, fleet.ErrTokenInvalid
	case err != nil:
		return 0, mapError(err)
	case row.ConsumedAt.Valid:
		return 0, fmt.Errorf("%w: already consumed", fleet.ErrTokenInvalid)
	case row.ExpiresAt.After(now):
		return 0, fmt.Errorf("%w: claimed by a concurrent registration", fleet.ErrTokenInvalid)
	default:
		return 0, fleet.ErrTokenExpired
	}
}

func (r tokenRepo) Unclaim(ctx context.Context, token string) error {
	return mapError(r.q.UnclaimToken(ctx, token))
}

func (r tokenRepo) Consume(ctx context.Context, token string) error {
	_, err := r.q.ConsumeToken(ctx, token)
	return mapError(err)
}
