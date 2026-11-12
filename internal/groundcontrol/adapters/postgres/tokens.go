package postgres

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// tokenRepo runs D7's ClaimToken, UnclaimToken and ConsumeToken. ClaimToken
// returns no row for a missing, claimed and expired token alike, so Claim
// reads the token with GetTokenByValue to tell them apart: missing is
// ErrTokenInvalid, claimed fmt.Errorf("%w: already consumed",
// ErrTokenInvalid) so the audit log keeps the reason while the response
// stays 400, expired ErrTokenExpired. This is the only place that
// classifies tokens once plan item 1.4 deletes server.claimZTRToken.
type tokenRepo struct{}

func (tokenRepo) Add(ctx context.Context, satellite fleet.SatelliteID, token string, expiresAt time.Time) error {
	return notimpl.Err("1.3 postgres add token")
}

func (tokenRepo) Claim(ctx context.Context, token string, now time.Time) (fleet.SatelliteID, error) {
	return 0, notimpl.Err("1.3 postgres claim token")
}

func (tokenRepo) Unclaim(ctx context.Context, token string) error {
	return notimpl.Err("1.3 postgres unclaim token")
}

func (tokenRepo) Consume(ctx context.Context, token string) error {
	return notimpl.Err("1.3 postgres consume token")
}
