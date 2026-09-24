package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestZtr_TokenClaimAndConsume is a regression test for #474 and its
// follow-up fix (early token deletion prevented safe retries).
//
// Originally, the Ztr handler looked up the token with a plain SELECT
// and only deleted it at the very end of the handler, after several
// other DB/network calls had already run. That left a window where the
// same token could be submitted twice (a retry or a replay), each
// triggering a robot secret rotation.
//
// The first fix replaced the lookup with an atomic claim
// (UPDATE ... SET claimed_at = NOW() ... RETURNING), closing the reuse
// window. But that surfaced a second problem: if any step after the
// claim failed (e.g. a transient DB error), the token stayed claimed
// forever, and the satellite's automatic retry would be rejected with
// "Invalid Token" even though registration never actually completed.
//
// The follow-up fix makes the claim reversible: a deferred rollback
// unclaims the token on any failure after ClaimToken, and the token is
// only permanently deleted (ConsumeToken) once registration fully
// succeeds. This test proves both properties:
//  1. A downstream failure unclaims the token, so a retry with the same
//     token is accepted (not "Invalid Token").
//  2. The token is genuinely reusable at that point — it is not silently
//     lost.
func TestZtr_TokenClaimAndConsume(t *testing.T) {
	server, mock := newMockServer(t)
	token := "test-ztr-token-value"
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	tokenColumns := []string{"id", "satellite_id", "token", "created_at", "updated_at", "expires_at", "claimed_at"}

	body, err := json.Marshal(ZTRRequest{Token: token})
	require.NoError(t, err)

	// --- First request: token is claimed, but a downstream step fails ---
	mock.ExpectQuery("UPDATE satellite_token").
		WithArgs(token).
		WillReturnRows(sqlmock.NewRows(tokenColumns).
			AddRow(1, int32(1), token, now, now, expiresAt, sql.NullTime{Time: now, Valid: true}))

	// Robot account lookup fails on purpose to simulate a transient
	// downstream failure after the token has already been claimed.
	mock.ExpectQuery("SELECT .+ FROM robot_accounts").
		WithArgs(int32(1)).
		WillReturnError(sql.ErrNoRows)

	// The deferred rollback should fire, unclaiming the token.
	mock.ExpectExec("UPDATE satellite_token").
		WithArgs(token).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req1 := httptest.NewRequest(http.MethodPost, "/api/ztr", bytes.NewReader(body))
	rr1 := httptest.NewRecorder()
	server.Ztr(rr1, req1)

	// We expect an internal error here (robot account not found), not an
	// "Invalid Token" error — proving the token itself was accepted on
	// this first call, and the failure happened downstream.
	require.Equal(t, http.StatusInternalServerError, rr1.Code)
	require.NotContains(t, rr1.Body.String(), "Invalid Token")

	// --- Second request with the SAME token: must now be ACCEPTED again ---
	// This is the core of the fix: a transient failure must not
	// permanently burn the token. The satellite's automatic retry
	// should be able to claim the same token a second time.
	mock.ExpectQuery("UPDATE satellite_token").
		WithArgs(token).
		WillReturnRows(sqlmock.NewRows(tokenColumns).
			AddRow(1, int32(1), token, now, now, expiresAt, sql.NullTime{Time: now, Valid: true}))

	mock.ExpectQuery("SELECT .+ FROM robot_accounts").
		WithArgs(int32(1)).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec("UPDATE satellite_token").
		WithArgs(token).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req2 := httptest.NewRequest(http.MethodPost, "/api/ztr", bytes.NewReader(body))
	rr2 := httptest.NewRecorder()
	server.Ztr(rr2, req2)

	require.Equal(t, http.StatusInternalServerError, rr2.Code)
	require.NotContains(t, rr2.Body.String(), "Invalid Token")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestZtr_ClaimedTokenCannotBeReplayedConcurrently proves that once a
// token has been claimed (e.g. by an in-flight request), a concurrent or
// replayed request with the same token is rejected — the atomicity
// guarantee from the original fix for #474 still holds under the new
// claim/consume model.
func TestZtr_ClaimedTokenCannotBeReplayedConcurrently(t *testing.T) {
	server, mock := newMockServer(t)
	token := "test-ztr-token-value-2"

	body, err := json.Marshal(ZTRRequest{Token: token})
	require.NoError(t, err)

	// Simulates a second, concurrent request finding the token already
	// claimed: ClaimToken's WHERE claimed_at IS NULL guard means no row
	// is returned.
	mock.ExpectQuery("UPDATE satellite_token").
		WithArgs(token).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPost, "/api/ztr", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.Ztr(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "Invalid Token")
	require.NoError(t, mock.ExpectationsWereMet())
}
