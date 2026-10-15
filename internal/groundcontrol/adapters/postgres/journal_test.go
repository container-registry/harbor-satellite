package postgres_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// A journaled heartbeat's ingest costs BEGIN, OpenJournalCursor,
// AdvanceJournalCursor and COMMIT (A99).
func TestRepository_JournalIngestTakesFourStatementsAndEmitsOnce(t *testing.T) {
	pending.Plan(t, "6.5", "the postgres journal repository is a stub")
	db := dbtest.New(t)
	ctx := context.Background()
	sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	id := fleet.SatelliteID(sat.ID)
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	events := []fleet.EdgeEvent{
		{Kind: fleet.EventEdgeReplicated, Payload: map[string]any{"name": "edge-01", "seq": 1}},
		{Kind: fleet.EventEdgeDeleted, Payload: map[string]any{"name": "edge-01", "seq": 2}},
	}

	used := db.MeasureStatements(t, func() {
		require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error {
			cursor, err := tx.Journal().OpenCursor(ctx, id, 1)
			require.NoError(t, err)
			require.Equal(t, fleet.JournalCursor{Epoch: 1, Fresh: true}, cursor)
			return tx.Journal().Advance(ctx, id, 1, 2, events)
		}))
	})

	require.LessOrEqual(t, used.Total, int64(4), used.String())
	require.EqualValues(t, 1, used.Calls["OpenJournalCursor"], used.String())
	require.EqualValues(t, 1, used.Calls["AdvanceJournalCursor"], used.String())
	countEdgeEvents := func() int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx,
			"SELECT count(*) FROM event_outbox WHERE aggregate_type = 'edge' AND aggregate_id = $1 AND done_at IS NOT NULL", sat.ID).Scan(&n))
		return n
	}
	require.Equal(t, 2, countEdgeEvents())

	journal := repo.Autocommit().Journal()
	require.ErrorIs(t, journal.Advance(ctx, id, 1, 2, events), fleet.ErrConflict, "a cursor that does not move")
	require.Equal(t, 2, countEdgeEvents(), "emits nothing")
	require.ErrorIs(t, journal.Advance(ctx, id, 1, 3, []fleet.EdgeEvent{{Kind: fleet.EventSatelliteCreated}}), fleet.ErrInvalid,
		"Advance appends edge events only")
	var outbox int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM event_outbox").Scan(&outbox))
	require.Equal(t, 2, outbox)
	cursor, err := journal.OpenCursor(ctx, id, 2)
	require.NoError(t, err)
	require.Equal(t, fleet.JournalCursor{Epoch: 2, Fresh: true}, cursor, "a newer epoch resets the cursor")
	cursor, err = journal.OpenCursor(ctx, id, 1)
	require.NoError(t, err)
	require.Equal(t, fleet.JournalCursor{Epoch: 2}, cursor, "an older epoch leaves it")
}
