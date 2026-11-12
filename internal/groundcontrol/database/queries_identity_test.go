package database_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/stretchr/testify/require"
)

var identityNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func oidcUser(subject, username string, groups ...string) database.UpsertOIDCUserParams {
	if groups == nil {
		groups = []string{}
	}
	return database.UpsertOIDCUserParams{
		Groups: groups, Now: identityNow, Issuer: "https://dex.example", Subject: subject, Username: username, Role: "admin",
	}
}

func TestUpsertOIDCUser_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()

	id, err := q.UpsertOIDCUser(ctx, oidcUser("sub-alice", "alice", "edge-team"))
	require.NoError(t, err)
	user, err := q.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, user.ID, id, "a new identity creates the user")
	require.Equal(t, "admin", user.Role)
	require.False(t, auth.VerifyPassword("!oidc", user.PasswordHash), "a JIT user has no usable password")
	require.False(t, auth.VerifyPassword("", user.PasswordHash))

	again, err := q.UpsertOIDCUser(ctx, oidcUser("sub-alice", "alice-renamed", "ops"))
	require.NoError(t, err)
	require.Equal(t, id, again, "the same identity is the same user")
	var groups []string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT array_to_json(groups) FROM user_identities WHERE subject = 'sub-alice'").Scan(jsonScanner{&groups}))
	require.Equal(t, []string{"ops"}, groups, "groups are refreshed")
	_, err = q.GetUserByUsername(ctx, "alice-renamed")
	require.ErrorIs(t, err, sql.ErrNoRows, "a known identity creates no second user")

	promoted := oidcUser("sub-alice", "alice", "ops")
	promoted.Role = "system_admin"
	_, err = q.UpsertOIDCUser(ctx, promoted)
	require.NoError(t, err)
	user, err = q.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "system_admin", user.Role, "every login re-derives the role")
	_, err = q.UpsertOIDCUser(ctx, oidcUser("sub-alice", "alice", "ops"))
	require.NoError(t, err)
	user, err = q.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "admin", user.Role, "losing the group at the provider demotes at the next login")

	_, err = q.CreateUser(ctx, database.CreateUserParams{Username: "bob", PasswordHash: "local-hash", Role: "admin"})
	require.NoError(t, err)
	_, err = q.UpsertOIDCUser(ctx, oidcUser("sub-bob", "bob"))
	require.ErrorIs(t, err, sql.ErrNoRows, "a local account is never adopted")
	var identities int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM user_identities").Scan(&identities))
	require.Equal(t, 1, identities)

	session, err := q.CreateSession(ctx, database.CreateSessionParams{UserID: id, Token: "tok-alice", ExpiresAt: identityNow.Add(time.Hour)})
	require.NoError(t, err)
	_, err = q.GetSessionPrincipalByToken(ctx, database.GetSessionPrincipalByTokenParams{Token: session.Token, Now: identityNow.Add(2 * time.Hour)})
	require.ErrorIs(t, err, sql.ErrNoRows, "expiry is checked against the given now")
	principal, err := q.GetSessionPrincipalByToken(ctx, database.GetSessionPrincipalByTokenParams{Token: session.Token, Now: identityNow})
	require.NoError(t, err)
	require.Equal(t, "alice", principal.Username)
	require.Equal(t, []string{"ops"}, principal.Groups)
}

type jsonScanner struct{ v any }

func (s jsonScanner) Scan(src any) error {
	switch b := src.(type) {
	case []byte:
		return json.Unmarshal(b, s.v)
	case string:
		return json.Unmarshal([]byte(b), s.v)
	}
	return nil
}

func TestTakeOIDCLoginState_SingleUseBindingAndExpiry(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	binding := sha256.Sum256([]byte("cookie"))
	other := sha256.Sum256([]byte("other cookie"))
	require.NoError(t, q.InsertOIDCLoginState(ctx, database.InsertOIDCLoginStateParams{
		State: "s1", CodeVerifier: "verifier", Nonce: "nonce", BindingHash: binding[:], ExpiresAt: identityNow.Add(10 * time.Minute),
	}))
	require.NoError(t, q.InsertOIDCLoginState(ctx, database.InsertOIDCLoginStateParams{
		State: "s2", CodeVerifier: "v2", Nonce: "n2", BindingHash: binding[:], ExpiresAt: identityNow.Add(-time.Second),
	}))
	take := func(state string, hash [32]byte) (database.TakeOIDCLoginStateRow, error) {
		return q.TakeOIDCLoginState(ctx, database.TakeOIDCLoginStateParams{State: state, BindingHash: hash[:], Now: identityNow})
	}

	_, err := take("s1", other)
	require.ErrorIs(t, err, sql.ErrNoRows, "another browser cannot complete the login")
	got, err := take("s1", binding)
	require.NoError(t, err)
	require.Equal(t, database.TakeOIDCLoginStateRow{CodeVerifier: "verifier", Nonce: "nonce"}, got)
	_, err = take("s1", binding)
	require.ErrorIs(t, err, sql.ErrNoRows, "single use")
	_, err = take("s2", binding)
	require.ErrorIs(t, err, sql.ErrNoRows, "expired")
	short := sha256.Sum256(nil)
	err = q.InsertOIDCLoginState(ctx, database.InsertOIDCLoginStateParams{State: "s3", CodeVerifier: "v", Nonce: "n", BindingHash: short[:31], ExpiresAt: identityNow})
	require.Error(t, err, "the binding hash is 32 bytes")

	require.NoError(t, q.InsertOIDCLoginState(ctx, database.InsertOIDCLoginStateParams{
		State: "s4", CodeVerifier: "v4", Nonce: "n4", BindingHash: binding[:], ExpiresAt: identityNow.Add(time.Minute),
	}))
	n, err := q.DeleteExpiredOIDCLoginStates(ctx, identityNow)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "a state that was never taken stays until it expires: s2")
	var left []string
	rows, err := db.QueryContext(ctx, "SELECT state FROM oidc_login_states")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		left = append(left, s)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"s4"}, left)
}

func TestRobotRotations_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	sat, err := q.CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName: "robot$edge-01", RobotSecretHash: "old-hash", RobotID: "42", SatelliteID: sat.ID,
		RobotExpiry: sql.NullTime{Time: identityNow.Add(24 * time.Hour), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, q.IssueSatelliteSession(ctx, database.IssueSatelliteSessionParams{
		SatelliteID: sat.ID, TokenHash: tokenHash("hss_edge-01"), ExpiresAt: identityNow.Add(time.Hour),
	}))
	basic, err := q.CreateSatellite(ctx, "edge-basic")
	require.NoError(t, err)
	_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName: "robot$edge-basic", RobotSecretHash: "basic-hash", RobotID: "50", SatelliteID: basic.ID,
		RobotExpiry: sql.NullTime{Time: identityNow.Add(24 * time.Hour), Valid: true},
	})
	require.NoError(t, err)
	expiring := func() []database.ListRobotsExpiringBeforeRow {
		t.Helper()
		rows, err := q.ListRobotsExpiringBefore(ctx, database.ListRobotsExpiringBeforeParams{Before: identityNow.Add(72 * time.Hour), Now: identityNow, RowLimit: 10})
		require.NoError(t, err)
		return rows
	}
	harborIDs := func() []int64 {
		t.Helper()
		ids, err := q.ListRobotHarborIDsOfSatellite(ctx, sat.ID)
		require.NoError(t, err)
		return ids
	}
	stage := func(generation, harborID int64) (int64, error) {
		return q.StageRobotRotation(ctx, database.StageRobotRotationParams{
			SatelliteID: sat.ID, Generation: generation, RobotName: "robot$edge-01-g" + string(rune('0'+generation)), RobotHarborID: harborID,
			SecretCiphertext: []byte("sealed"), SecretHash: "new-hash", ExpiresAt: sql.NullTime{Time: identityNow.Add(30 * 24 * time.Hour), Valid: true},
		})
	}

	rows := expiring()
	require.Len(t, rows, 1, "edge-basic has neither a session nor an SVID, so no beat could deliver its successor")
	require.Equal(t, sat.ID, rows[0].SatelliteID)
	require.Equal(t, int64(1), rows[0].NextGeneration)
	_, err = stage(2, 43)
	require.ErrorIs(t, err, sql.ErrNoRows, "a first rotation is generation 1")
	gen, err := stage(1, 43)
	require.NoError(t, err)
	require.Equal(t, int64(1), gen)
	require.Empty(t, expiring(), "a robot with an unpromoted rotation is not listed again")
	require.Equal(t, []int64{42, 43}, harborIDs(), "the tombstone covers the unpromoted successor")
	_, err = stage(2, 44)
	require.ErrorIs(t, err, sql.ErrNoRows, "one rotation in flight per satellite")

	pending, err := q.ListPendingRobotRotations(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	acks, err := json.Marshal([]map[string]int64{{"satellite_id": int64(sat.ID), "generation": 1}})
	require.NoError(t, err)
	require.NoError(t, q.AckRobotRotations(ctx, database.AckRobotRotationsParams{Now: identityNow, Acks: acks}))
	pending, err = q.ListPendingRobotRotations(ctx)
	require.NoError(t, err)
	require.Empty(t, pending)
	acked, err := q.ListAckedRobotRotations(ctx, 10)
	require.NoError(t, err)
	require.Len(t, acked, 1)
	require.Equal(t, int64(42), acked[0].PreviousHarborID)
	require.Equal(t, int64(43), acked[0].RobotHarborID)

	n, err := q.PromoteRobotRotation(ctx, database.PromoteRobotRotationParams{Now: identityNow, SatelliteID: sat.ID, Generation: 1, PreviousHarborID: 99})
	require.NoError(t, err)
	require.Zero(t, n, "robot_accounts does not hold that robot: nothing is marked promoted")
	acked, err = q.ListAckedRobotRotations(ctx, 10)
	require.NoError(t, err)
	require.Len(t, acked, 1, "still acked")
	n, err = q.PromoteRobotRotation(ctx, database.PromoteRobotRotationParams{Now: identityNow, SatelliteID: sat.ID, Generation: 1, PreviousHarborID: 42})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	robot, err := q.GetRobotAccBySatelliteID(ctx, sat.ID)
	require.NoError(t, err)
	require.Equal(t, "43", robot.RobotID)
	require.Equal(t, "new-hash", robot.RobotSecretHash)
	var ciphertext []byte
	require.NoError(t, db.QueryRowContext(ctx, "SELECT secret_ciphertext FROM robot_rotations WHERE satellite_id = $1", sat.ID).Scan(&ciphertext))
	require.Empty(t, ciphertext, "the sealed secret is dropped on promote")
	require.Equal(t, []int64{43}, harborIDs())
	n, err = q.PromoteRobotRotation(ctx, database.PromoteRobotRotationParams{Now: identityNow, SatelliteID: sat.ID, Generation: 1, PreviousHarborID: 42})
	require.NoError(t, err)
	require.Zero(t, n, "promote is idempotent")

	_, err = stage(3, 45)
	require.ErrorIs(t, err, sql.ErrNoRows, "the generation follows the last one")
	gen, err = stage(2, 44)
	require.NoError(t, err)
	require.Equal(t, int64(2), gen)
}
