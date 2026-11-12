package postgres_test

import (
	"context"
	"fmt"
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

const pendingHeartbeat = "the postgres heartbeat store is a stub; the heartbeat writes 8 statements in the request"

func TestHeartbeatStore_FlushIsOneTransaction(t *testing.T) {
	pending.Plan(t, "4.2", pendingHeartbeat)
	db := dbtest.New(t)
	ctx := context.Background()
	q := database.New(db.DB)
	store, err := postgres.NewHeartbeatStore(db.DB, fleettest.Clock{T: hotNow})
	require.NoError(t, err)
	var batch heartbeat.Batch
	for i := range 10 {
		sat, err := q.CreateSatellite(ctx, fmt.Sprintf("edge-%02d", i))
		require.NoError(t, err)
		id := fleet.SatelliteID(sat.ID)
		seen := hotNow.Add(-time.Duration(i) * time.Second)
		batch.Latest = append(batch.Latest, fleet.Heartbeat{Satellite: id, SeenAt: seen, Interval: "@every 00h00m30s", StateDigest: "sha256:s"})
		batch.Samples = append(batch.Samples, fleet.StatusSample{Satellite: id, Activity: "idle", ReportedAt: seen})
		batch.Inventories = append(batch.Inventories, heartbeat.InventoryUpdate{Satellite: id, Digest: "sha256:inv", Items: []fleet.InventoryItem{{Reference: "library/alpine:3.20", SizeBytes: 3 << 20}}})
	}

	used := db.MeasureStatements(t, func() { require.NoError(t, store.Flush(ctx, batch)) })

	require.LessOrEqual(t, used.Total, int64(6), "BEGIN, four batch statements, COMMIT for 10 beats\n%s", used)
	for _, name := range []string{"FlushLastSeen", "FlushSatelliteView", "InsertSatelliteStatusBatch", "StoreSatelliteInventories"} {
		require.EqualValues(t, 1, used.Calls[name], "%s\n%s", name, used)
	}
	var views int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM satellite_view WHERE state = 'online'").Scan(&views))
	require.Equal(t, 10, views)
}
