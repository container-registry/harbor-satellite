package fleettest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

func TestJournalRepo_RecordsAndAnswers(t *testing.T) {
	ctx := context.Background()
	repo := fleettest.NewRepository(nil)
	repo.Journal.Cursor = fleet.JournalCursor{Epoch: 42, AckedSeq: 3}
	events := []fleet.EdgeEvent{{Kind: fleet.EventEdgeReplicated, Payload: map[string]any{"seq": 4}}}

	err := repo.WithTx(ctx, func(tx fleet.Tx) error {
		cursor, err := tx.Journal().OpenCursor(ctx, 7, 42)
		require.NoError(t, err)
		require.Equal(t, fleet.JournalCursor{Epoch: 42, AckedSeq: 3}, cursor)
		return tx.Journal().Advance(ctx, 7, 42, 5, events)
	})
	require.NoError(t, err)

	require.Equal(t, []string{"WithTx", "Journal.OpenCursor", "Journal.Advance", "Commit"}, repo.Methods())
	require.Equal(t, []any{fleet.SatelliteID(7), int64(42), int64(5), events}, repo.CallsTo("Journal.Advance")[0].Args)
	require.Equal(t, events, repo.Journal.Advanced())

	boom := errors.New("cursor locked")
	repo.FailOn("Journal.OpenCursor", boom)
	_, err = repo.Autocommit().Journal().OpenCursor(ctx, 7, 42)
	require.ErrorIs(t, err, boom)
}
