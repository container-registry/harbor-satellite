package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/heartbeat"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestLifecycle_PendingOnlineStaleOnlineRevokedPending(t *testing.T) {
	pending.Plan(t, "4.3", pendingViews)
	db := dbtest.New(t)
	ctx := context.Background()
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	sat, err := database.New(db.DB).CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	id := fleet.SatelliteID(sat.ID)
	flush := func(seen time.Time) {
		t.Helper()
		store, err := postgres.NewHeartbeatStore(db.DB, fleettest.Clock{T: seen})
		require.NoError(t, err)
		require.NoError(t, store.Flush(ctx, heartbeat.Batch{
			Latest:  []fleet.Heartbeat{{Satellite: id, SeenAt: seen, Interval: "@every 00h00m30s"}},
			Samples: []fleet.StatusSample{{Satellite: id, Activity: "idle", ReportedAt: seen}},
		}))
	}
	markStale := func(now time.Time) {
		t.Helper()
		require.NoError(t, repo.WithTx(ctx, func(tx fleet.Tx) error {
			_, err := tx.Views().MarkStale(ctx, now, 15*time.Minute, 10*time.Second)
			return err
		}))
	}

	require.Equal(t, fleet.StatePending, viewState(t, repo, id))
	flush(hotNow)
	require.Equal(t, fleet.StateOnline, viewState(t, repo, id))
	markStale(hotNow.Add(2 * time.Minute))
	require.Equal(t, fleet.StateStale, viewState(t, repo, id), "silent for more than 3 intervals")
	flush(hotNow.Add(3 * time.Minute))
	require.Equal(t, fleet.StateOnline, viewState(t, repo, id))

	previous, err := repo.Autocommit().Views().Revoke(ctx, id, hotNow.Add(4*time.Minute))
	require.NoError(t, err)
	require.Equal(t, fleet.StateOnline, previous)
	flush(hotNow.Add(5 * time.Minute))
	require.Equal(t, fleet.StateRevoked, viewState(t, repo, id), "a beat never leaves revoked")
	markStale(hotNow.Add(time.Hour))
	require.Equal(t, fleet.StateRevoked, viewState(t, repo, id), "a revoked satellite is never stale")

	changed, err := repo.Autocommit().Views().Unrevoke(ctx, id, hotNow.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, fleet.StatePending, viewState(t, repo, id))
}

func TestSatelliteRepo_SetIdentityAndGetFillsSPIFFEID(t *testing.T) {
	pending.Plan(t, "4.3", "satellite_identities is not read or written")
	db := dbtest.New(t)
	ctx := context.Background()
	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	sats := repo.Autocommit().Satellites()
	spiffe, err := sats.Create(ctx, "edge-01")
	require.NoError(t, err)
	token, err := sats.Create(ctx, "edge-02")
	require.NoError(t, err)
	spiffeID := "spiffe://harbor-satellite.local/satellite/region/default/edge-01"

	require.NoError(t, sats.SetIdentity(ctx, spiffe.ID, spiffeID, []string{"entry-1"}))

	got, err := sats.Get(ctx, spiffe.ID)
	require.NoError(t, err)
	require.Equal(t, spiffeID, got.SPIFFEID)
	got, err = sats.GetByName(ctx, "edge-01")
	require.NoError(t, err)
	require.Equal(t, spiffeID, got.SPIFFEID)
	got, err = sats.Get(ctx, token.ID)
	require.NoError(t, err)
	require.Empty(t, got.SPIFFEID)
	all, err := sats.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestDeleteOrphanedArtifacts_KeepsViewInventory(t *testing.T) {
	pending.Plan(t, "4.4", "DeleteOrphanedArtifacts reads only satellite_status.artifact_ids")
	db := dbtest.New(t)
	ctx := context.Background()
	q := database.New(db.DB)
	sat, err := q.CreateSatellite(ctx, "edge-01")
	require.NoError(t, err)
	var kept, orphan int32
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO artifacts (reference, size_bytes, created_at) VALUES ('library/alpine:3.20', 1, NOW() - INTERVAL '30 days') RETURNING id").Scan(&kept))
	require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO artifacts (reference, size_bytes, created_at) VALUES ('library/busybox:1.36', 1, NOW() - INTERVAL '30 days') RETURNING id").Scan(&orphan))
	_, err = db.ExecContext(ctx, "INSERT INTO satellite_view (satellite_id, state, inventory_artifact_ids) VALUES ($1, 'online', ARRAY[$2::INT])", sat.ID, kept)
	require.NoError(t, err)

	require.NoError(t, q.DeleteOrphanedArtifacts(ctx, 7))

	var ids []int32
	rows, err := db.QueryContext(ctx, "SELECT id FROM artifacts ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id int32
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int32{kept}, ids)
}
