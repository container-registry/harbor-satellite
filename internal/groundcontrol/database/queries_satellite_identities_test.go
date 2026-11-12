package database_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSatelliteIdentities_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	const spiffeID = "spiffe://harbor-satellite.local/satellite/region/default/edge-01"
	sat, err := q.CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	tokenSat, err := q.CreateSatellite(ctx, "edge-02")
	require.NoError(t, err)

	require.NoError(t, q.UpsertSatelliteIdentity(ctx, database.UpsertSatelliteIdentityParams{
		SatelliteID: sat.ID, SpiffeID: spiffeID, SpireEntryIds: []string{"entry-1"},
	}))
	require.NoError(t, q.UpsertSatelliteIdentity(ctx, database.UpsertSatelliteIdentityParams{
		SatelliteID: sat.ID, SpiffeID: spiffeID, SpireEntryIds: []string{"entry-1", "entry-2"},
	}), "an upsert replaces the entry IDs")
	var entries []string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT spire_entry_ids FROM satellite_identities WHERE satellite_id = $1", sat.ID).
		Scan(pq.Array(&entries)))
	require.Equal(t, []string{"entry-1", "entry-2"}, entries)

	err = q.UpsertSatelliteIdentity(ctx, database.UpsertSatelliteIdentityParams{SatelliteID: tokenSat.ID, SpiffeID: spiffeID, SpireEntryIds: []string{}})
	require.ErrorContains(t, err, "satellite_identities_spiffe_id_key", "a SPIFFE ID belongs to one satellite")

	byID, err := q.GetFleetSatelliteByID(ctx, sat.ID)
	require.NoError(t, err)
	require.Equal(t, spiffeID, byID.SpiffeID)
	byName, err := q.GetFleetSatelliteByName(ctx, "edge-02")
	require.NoError(t, err)
	require.Empty(t, byName.SpiffeID, "a token satellite has none")
	all, err := q.ListFleetSatellites(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, []string{"edge-01", "edge-02"}, []string{all[0].Name, all[1].Name}, "ordered by id")
	require.Equal(t, spiffeID, all[0].SpiffeID)

	_, err = db.ExecContext(ctx, "DELETE FROM satellites WHERE id = $1", sat.ID)
	require.NoError(t, err)
	var left int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM satellite_identities").Scan(&left))
	require.Zero(t, left, "deleting the satellite deletes its identity")
	_, err = q.GetFleetSatelliteByID(ctx, sat.ID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
