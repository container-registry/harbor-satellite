package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

// mustWait fails when done delivers within 200ms: the operation under test
// did not block on the open transaction.
func mustWait[T any](t *testing.T, done <-chan T, what string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("%s did not wait for the open transaction", what)
	case <-time.After(200 * time.Millisecond):
	}
}

// Two first logins of one OIDC subject whose claims carry different
// usernames: the one that loses the (issuer, subject) race logs in as the
// user the winner linked.
func TestIdentityStore_ConcurrentFirstLoginsWithDifferentUsernames(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	store, err := postgres.NewIdentityStore(db.DB)
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	winner, err := database.New(db.DB).WithTx(tx).UpsertOIDCUser(ctx, database.UpsertOIDCUserParams{
		Groups: []string{}, Now: time.Now(), Role: "admin", Issuer: "https://dex.example", Subject: "sub-alice", Username: "alice",
	})
	require.NoError(t, err)
	type result struct {
		id  int32
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, upsertErr := store.Upsert(ctx, authn.Claims{Issuer: "https://dex.example", Subject: "sub-alice", Username: "alice.renamed"}, "admin")
		done <- result{id, upsertErr}
	}()
	mustWait(t, done, "Upsert")
	require.NoError(t, tx.Commit())

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, winner, got.id)
}

// Two uploads open the cursor of a satellite that has none: the one that
// waited finds the cursor the first created and advanced, which is not
// fresh for it.
func TestJournal_ConcurrentFirstOpenIsNotFreshForTheWaiter(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	first := database.New(db.DB).WithTx(tx)
	opened, err := first.OpenJournalCursor(ctx, database.OpenJournalCursorParams{SatelliteID: sat.ID, Epoch: 1})
	require.NoError(t, err)
	require.True(t, opened.Fresh)
	_, err = first.AdvanceJournalCursor(ctx, database.AdvanceJournalCursorParams{SatelliteID: sat.ID, Epoch: 1, AckedSeq: 5, Events: []byte(`[]`)})
	require.NoError(t, err)
	type result struct {
		cursor fleet.JournalCursor
		err    error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		r.err = repo.WithTx(ctx, func(tx fleet.Tx) error {
			var openErr error
			r.cursor, openErr = tx.Journal().OpenCursor(ctx, fleet.SatelliteID(sat.ID), 1)
			return openErr
		})
		done <- r
	}()
	mustWait(t, done, "OpenCursor")
	require.NoError(t, tx.Commit())

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, fleet.JournalCursor{Epoch: 1, AckedSeq: 5}, got.cursor)
}

// A satellite deleted between reading its robot and storing the rotated
// secret leaves no row to update.
func TestRobotRepo_UpdateOfAMissingRobotIsNotFound(t *testing.T) {
	repo := newRepository(t)
	ctx := context.Background()
	tx := repo.Autocommit()
	sat, err := tx.Satellites().Create(ctx, "edge-01")
	require.NoError(t, err)
	robot, err := tx.Robots().Add(ctx, fleet.Robot{Satellite: sat.ID, Name: "robot$edge-01-1", HarborID: 42, SecretHash: "hash"})
	require.NoError(t, err)
	require.NoError(t, tx.Satellites().Delete(ctx, sat.ID))

	robot.SecretHash = "rotated"
	require.ErrorIs(t, tx.Robots().Update(ctx, robot), fleet.ErrNotFound)
}
