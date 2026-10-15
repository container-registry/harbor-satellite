package database_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

func TestClaimToken_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sat, err := q.CreateSatellite(ctx, "sat1")
	require.NoError(t, err)
	addToken := func(token string, expiresAt time.Time) {
		t.Helper()
		_, err := q.AddToken(ctx, database.AddTokenParams{SatelliteID: sat.ID, Token: token, ExpiresAt: expiresAt})
		require.NoError(t, err)
	}
	claim := func(token string) (int32, error) {
		return q.ClaimToken(ctx, database.ClaimTokenParams{Token: token, Now: time.Now()})
	}
	addToken("valid", time.Now().Add(time.Hour))
	addToken("expired", time.Now().Add(-time.Hour))
	addToken("unclaimed", time.Now().Add(time.Hour))

	id, err := claim("valid")
	require.NoError(t, err)
	require.Equal(t, sat.ID, id)
	_, err = claim("valid")
	require.ErrorIs(t, err, sql.ErrNoRows, "second claim")
	_, err = claim("expired")
	require.ErrorIs(t, err, sql.ErrNoRows, "expired token")
	_, err = claim("missing")
	require.ErrorIs(t, err, sql.ErrNoRows, "missing token")

	require.NoError(t, q.UnclaimToken(ctx, "valid"))
	id, err = claim("valid")
	require.NoError(t, err, "claim after unclaim")
	require.Equal(t, sat.ID, id)

	n, err := q.ConsumeToken(ctx, "unclaimed")
	require.NoError(t, err)
	require.Zero(t, n, "consume deleted an unclaimed token")
	n, err = q.ConsumeToken(ctx, "valid")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	_, err = q.GetTokenByValue(ctx, "valid")
	require.ErrorIs(t, err, sql.ErrNoRows)
}
