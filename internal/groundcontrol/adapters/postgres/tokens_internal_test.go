package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

// Another registration claimed the token, then failed and unclaimed it
// before this claimant read the row: the token is neither consumed nor
// expired, and it was claimed while this claim ran, so it is invalid.
func TestTokenRepo_ClaimLostToAnUnclaimedWinnerIsInvalid(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("-- name: ClaimToken").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("-- name: GetTokenByValue").WillReturnRows(sqlmock.NewRows(
		[]string{"id", "satellite_id", "token", "created_at", "updated_at", "expires_at", "consumed_at"}).
		AddRow(1, 7, "tok", now, now, now.Add(time.Hour), nil))

	_, err = tokenRepo{q: database.New(db)}.Claim(context.Background(), "tok", now)

	require.ErrorIs(t, err, fleet.ErrTokenInvalid)
	require.NotErrorIs(t, err, fleet.ErrTokenExpired)
	require.NoError(t, mock.ExpectationsWereMet())
}
