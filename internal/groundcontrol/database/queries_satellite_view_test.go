package database_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var viewNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// The rows the heartbeat flusher sends, as the batch statements read them.
type (
	lastSeenRow struct {
		ID                int32     `json:"id"`
		SeenAt            time.Time `json:"seen_at"`
		HeartbeatInterval string    `json:"heartbeat_interval"`
	}
	viewRow struct {
		ID                   int32           `json:"id"`
		SeenAt               time.Time       `json:"seen_at"`
		HeartbeatInterval    string          `json:"heartbeat_interval"`
		Activity             string          `json:"activity"`
		ReportedAt           time.Time       `json:"reported_at"`
		StateDigest          string          `json:"state_digest"`
		ConfigDigest         string          `json:"config_digest"`
		AppliedStates        json.RawMessage `json:"applied_states,omitempty"`
		InventoryDigest      string          `json:"inventory_digest"`
		LastReplicationError string          `json:"last_replication_error"`
	}
	statusRow struct {
		SatelliteID        int32    `json:"satellite_id"`
		Activity           string   `json:"activity"`
		LatestStateDigest  *string  `json:"latest_state_digest"`
		LatestConfigDigest *string  `json:"latest_config_digest"`
		CPUPercent         *float64 `json:"cpu_percent"`
		MemoryUsedBytes    *int64   `json:"memory_used_bytes"`
		StorageUsedBytes   *int64   `json:"storage_used_bytes"`
		LastSyncDurationMs *int64   `json:"last_sync_duration_ms"`
		ImageCount         *int32   `json:"image_count"`
		ReportedAt         string   `json:"reported_at"`
	}
	inventoryItem struct {
		Reference string `json:"reference"`
		SizeBytes int64  `json:"size_bytes"`
	}
	inventoryRow struct {
		SatelliteID int32           `json:"satellite_id"`
		Digest      string          `json:"digest"`
		Items       []inventoryItem `json:"items"`
	}
)

func rowsJSON(t *testing.T, rows any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(rows)
	require.NoError(t, err)
	return data
}

func createSatellites(t *testing.T, q *database.Queries, names ...string) []int32 {
	t.Helper()
	ids := make([]int32, 0, len(names))
	for _, name := range names {
		sat, err := q.CreateSatellite(context.Background(), name)
		require.NoError(t, err)
		ids = append(ids, sat.ID)
	}
	return ids
}

func flushView(t *testing.T, q *database.Queries, rows ...viewRow) {
	t.Helper()
	require.NoError(t, q.FlushSatelliteView(context.Background(), database.FlushSatelliteViewParams{Now: viewNow, Rows: rowsJSON(t, rows)}))
}

func viewOf(t *testing.T, q *database.Queries, id int32) database.GetSatelliteViewByIDRow {
	t.Helper()
	v, err := q.GetSatelliteViewByID(context.Background(), id)
	require.NoError(t, err)
	return v
}

func TestFlushLastSeen_OlderBeatChangesNothing(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	ids := createSatellites(t, q, "edge-01", "edge-02")
	flush := func(rows ...lastSeenRow) {
		t.Helper()
		require.NoError(t, q.FlushLastSeen(ctx, rowsJSON(t, rows)))
	}
	seen := func(id int32) database.Satellite {
		t.Helper()
		sat, err := q.GetSatellite(ctx, id)
		require.NoError(t, err)
		return sat
	}

	flush(lastSeenRow{ID: ids[0], SeenAt: viewNow, HeartbeatInterval: "@every 00h00m30s"},
		lastSeenRow{ID: ids[1], SeenAt: viewNow, HeartbeatInterval: ""},
		lastSeenRow{ID: 9999, SeenAt: viewNow, HeartbeatInterval: "@every 00h00m30s"})
	require.True(t, seen(ids[0]).LastSeen.Time.Equal(viewNow))
	require.Equal(t, "@every 00h00m30s", seen(ids[0]).HeartbeatInterval.String)
	require.True(t, seen(ids[1]).LastSeen.Valid)
	require.False(t, seen(ids[1]).HeartbeatInterval.Valid, "an empty interval is NULL, like UpdateSatelliteLastSeen")

	flush(lastSeenRow{ID: ids[0], SeenAt: viewNow.Add(-time.Minute), HeartbeatInterval: "@every 00h01m00s"})
	require.True(t, seen(ids[0]).LastSeen.Time.Equal(viewNow), "an older beat never moves last_seen back")
	require.Equal(t, "@every 00h00m30s", seen(ids[0]).HeartbeatInterval.String)

	flush(lastSeenRow{ID: ids[0], SeenAt: viewNow.Add(time.Second), HeartbeatInterval: "@every 00h01m00s"})
	require.True(t, seen(ids[0]).LastSeen.Time.Equal(viewNow.Add(time.Second)))
	require.Equal(t, "@every 00h01m00s", seen(ids[0]).HeartbeatInterval.String)
}

func TestFlushSatelliteView_OlderBeatChangesNothing(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	id := createSatellites(t, q, "edge-01")[0]

	flushView(t, q, viewRow{ID: id, SeenAt: viewNow, HeartbeatInterval: "@every 00h00m30s", Activity: "replicating", ReportedAt: viewNow, StateDigest: "sha256:new"})
	flushView(t, q, viewRow{ID: id, SeenAt: viewNow.Add(-time.Minute), HeartbeatInterval: "@every 00h01m00s", Activity: "older", ReportedAt: viewNow.Add(-time.Minute), StateDigest: "sha256:old"})

	v := viewOf(t, q, id)
	require.Equal(t, "online", v.State)
	require.Equal(t, "replicating", v.LastActivity.String)
	require.Equal(t, "sha256:new", v.AppliedStateDigest.String)
	var lastSeen time.Time
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT last_seen FROM satellite_view WHERE satellite_id = $1", id).Scan(&lastSeen))
	require.True(t, lastSeen.Equal(viewNow))
}

func TestFlushSatelliteView_KeepsRevokedSkipsDeletedSatellite(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	ids := createSatellites(t, q, "revoked", "stale", "pending")
	_, err := q.RevokeSatelliteView(ctx, database.RevokeSatelliteViewParams{SatelliteID: ids[0], Now: viewNow.Add(-time.Hour)})
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO satellite_view (satellite_id, state, last_seen) VALUES ($1, 'stale', $2)", ids[1], viewNow.Add(-time.Hour))
	require.NoError(t, err)

	flushView(t, q,
		viewRow{ID: ids[0], SeenAt: viewNow, Activity: "a"},
		viewRow{ID: ids[1], SeenAt: viewNow, Activity: "b"},
		viewRow{ID: ids[2], SeenAt: viewNow, Activity: "c"},
		viewRow{ID: 9999, SeenAt: viewNow, Activity: "deleted meanwhile"})

	require.Equal(t, "revoked", viewOf(t, q, ids[0]).State, "a beat never unrevokes")
	require.Equal(t, "a", viewOf(t, q, ids[0]).LastActivity.String, "but it is recorded")
	require.Equal(t, "online", viewOf(t, q, ids[1]).State)
	require.Equal(t, "online", viewOf(t, q, ids[2]).State)
	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM satellite_view").Scan(&rows))
	require.Equal(t, 3, rows, "no row for a satellite deleted between its beat and the flush")
}

func TestFlushSatelliteView_StoresAppliedStates(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	id := createSatellites(t, q, "edge-01")[0]
	applied := json.RawMessage(`[{"ref":"http://harbor/satellite/group-state/g1/state:latest","digest":"sha256:g1"}]`)

	flushView(t, q, viewRow{
		ID: id, SeenAt: viewNow, StateDigest: "sha256:s", ConfigDigest: "sha256:c",
		AppliedStates: applied, InventoryDigest: "sha256:inv", LastReplicationError: "pull failed",
	})
	v := viewOf(t, q, id)
	require.JSONEq(t, string(applied), string(v.AppliedStates))
	require.Equal(t, "sha256:s", v.AppliedStateDigest.String)
	require.Equal(t, "sha256:c", v.AppliedConfigDigest.String)
	require.Equal(t, "sha256:inv", v.InventoryDigest.String)
	require.Equal(t, "pull failed", v.LastReplicationError.String)

	flushView(t, q, viewRow{ID: id, SeenAt: viewNow.Add(time.Second)})
	v = viewOf(t, q, id)
	require.Equal(t, "sha256:s", v.AppliedStateDigest.String, "a beat without digests keeps them")
	require.Equal(t, "sha256:c", v.AppliedConfigDigest.String)
	require.Equal(t, "sha256:inv", v.InventoryDigest.String)
	require.JSONEq(t, `[]`, string(v.AppliedStates), "applied_states is what the last beat reported")
	require.False(t, v.LastReplicationError.Valid, "a clean run clears the error")
}

func TestInsertSatelliteStatusBatch_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	id := createSatellites(t, q, "edge-01")[0]
	digest, cpu, mem, count := "sha256:s", 12.5, int64(1<<20), int32(2)

	require.NoError(t, q.InsertSatelliteStatusBatch(ctx, rowsJSON(t, []statusRow{
		{SatelliteID: id, Activity: "first", ReportedAt: "2026-10-15T11:59:00Z"},
		{SatelliteID: id, Activity: "second", LatestStateDigest: &digest, CPUPercent: &cpu, MemoryUsedBytes: &mem, ImageCount: &count, ReportedAt: "2026-10-15T12:00:00Z"},
		{SatelliteID: 9999, Activity: "deleted meanwhile", ReportedAt: "2026-10-15T12:00:00Z"},
	})))

	rows, err := q.GetSatelliteStatusHistory(ctx, database.GetSatelliteStatusHistoryParams{SatelliteID: id, Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 2, "every sample of a flush is kept, the deleted satellite's is skipped")
	byActivity := map[string]database.SatelliteStatus{}
	for _, r := range rows {
		byActivity[r.Activity] = r
		require.Nil(t, r.ArtifactIds, "the inventory lives in satellite_view")
	}
	first, second := byActivity["first"], byActivity["second"]
	require.False(t, first.LatestStateDigest.Valid, "zero values arrive as null")
	require.False(t, first.CpuPercent.Valid)
	require.False(t, first.ImageCount.Valid)
	require.Equal(t, "sha256:s", second.LatestStateDigest.String)
	require.Equal(t, "12.50", second.CpuPercent.String)
	require.Equal(t, mem, second.MemoryUsedBytes.Int64)
	require.Equal(t, count, second.ImageCount.Int32)
	require.True(t, second.ReportedAt.Equal(viewNow))
}

func artifactIDs(t *testing.T, db *dbtest.DB, refs ...string) map[string]int32 {
	t.Helper()
	out := map[string]int32{}
	for _, ref := range refs {
		var id int32
		require.NoError(t, db.QueryRowContext(context.Background(), "SELECT id FROM artifacts WHERE reference = $1", ref).Scan(&id))
		out[ref] = id
	}
	return out
}

func inventoryIDs(t *testing.T, db *dbtest.DB, satellite int32) []int32 {
	t.Helper()
	var ids []int32
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT inventory_artifact_ids FROM satellite_view WHERE satellite_id = $1", satellite).Scan((*pq.Int32Array)(&ids)))
	return ids
}

func TestStoreSatelliteInventories_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	ids := createSatellites(t, q, "edge-01", "edge-02")
	require.NoError(t, q.BatchInsertArtifacts(ctx, database.BatchInsertArtifactsParams{Refs: []string{"h/lib/y:1"}, Sizes: []int64{2}}))
	existing := artifactIDs(t, db, "h/lib/y:1")["h/lib/y:1"]

	require.NoError(t, q.StoreSatelliteInventories(ctx, database.StoreSatelliteInventoriesParams{Now: viewNow, Rows: rowsJSON(t, []inventoryRow{
		{SatelliteID: ids[0], Digest: "sha256:a", Items: []inventoryItem{{"h/lib/x:1", 1}, {"h/lib/y:1", 2}}},
		{SatelliteID: ids[1], Digest: "sha256:b", Items: []inventoryItem{{"h/lib/z:1", 3}, {"h/lib/y:1", 2}}},
		{SatelliteID: 9999, Digest: "sha256:gone", Items: []inventoryItem{{"h/lib/gone:1", 4}}},
	})}))

	got := artifactIDs(t, db, "h/lib/x:1", "h/lib/y:1", "h/lib/z:1")
	require.Equal(t, existing, got["h/lib/y:1"], "an existing reference keeps its id")
	require.ElementsMatch(t, []int32{got["h/lib/x:1"], got["h/lib/y:1"]}, inventoryIDs(t, db, ids[0]))
	require.ElementsMatch(t, []int32{got["h/lib/y:1"], got["h/lib/z:1"]}, inventoryIDs(t, db, ids[1]))
	var gone int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM artifacts WHERE reference = 'h/lib/gone:1'").Scan(&gone))
	require.Zero(t, gone, "a deleted satellite's inventory is skipped")
	v := viewOf(t, q, ids[0])
	require.Equal(t, "sha256:a", v.InventoryDigest.String)
	require.Equal(t, "pending", v.State, "an inventory alone does not bring a satellite online")

	listed, err := q.ListSatelliteInventory(ctx, ids[0])
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, "h/lib/x:1", listed[0].Reference)
	require.Equal(t, "h/lib/y:1", listed[1].Reference)

	require.NoError(t, q.StoreSatelliteInventories(ctx, database.StoreSatelliteInventoriesParams{Now: viewNow, Rows: rowsJSON(t, []inventoryRow{
		{SatelliteID: ids[0], Digest: "sha256:empty", Items: []inventoryItem{}},
	})}))
	require.Empty(t, inventoryIDs(t, db, ids[0]), "empty items clear the inventory")
	require.Equal(t, "sha256:empty", viewOf(t, q, ids[0]).InventoryDigest.String)
}

// A114: a replica that waits on another's insert of the same reference
// still stores its id.
func TestStoreSatelliteInventories_ConcurrentReplicasGetIDs(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	ids := createSatellites(t, q, "edge-01", "edge-02")
	store := func(q *database.Queries, satellite int32) error {
		return q.StoreSatelliteInventories(ctx, database.StoreSatelliteInventoriesParams{Now: viewNow, Rows: rowsJSON(t, []inventoryRow{
			{SatelliteID: satellite, Digest: "sha256:same", Items: []inventoryItem{{"h/lib/shared:1", 1}}},
		})})
	}
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Error(err)
		}
	})
	require.NoError(t, store(q.WithTx(tx), ids[0]))

	waiting := make(chan error, 1)
	go func() { waiting <- store(q, ids[1]) }()
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-waiting)

	shared := artifactIDs(t, db, "h/lib/shared:1")["h/lib/shared:1"]
	require.Equal(t, []int32{shared}, inventoryIDs(t, db, ids[0]))
	require.Equal(t, []int32{shared}, inventoryIDs(t, db, ids[1]), "DO UPDATE returns the id the other replica inserted")
}

func TestRevokeUnrevokeSatelliteView_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	ids := createSatellites(t, q, "edge-01", "edge-02")
	revoke := func(id int32, now time.Time) string {
		t.Helper()
		prev, err := q.RevokeSatelliteView(ctx, database.RevokeSatelliteViewParams{SatelliteID: id, Now: now})
		require.NoError(t, err)
		return prev
	}
	unrevoke := func(id int32) int64 {
		t.Helper()
		n, err := q.UnrevokeSatelliteView(ctx, database.UnrevokeSatelliteViewParams{SatelliteID: id, Now: viewNow})
		require.NoError(t, err)
		return n
	}

	require.Equal(t, "pending", revoke(ids[0], viewNow), "no view row is pending")
	require.Equal(t, "revoked", revoke(ids[0], viewNow.Add(time.Hour)))
	v := viewOf(t, q, ids[0])
	require.Equal(t, "revoked", v.State)
	require.True(t, v.RevokedAt.Time.Equal(viewNow), "revoking twice keeps the first revoked_at")

	flushView(t, q, viewRow{ID: ids[1], SeenAt: viewNow})
	require.Equal(t, "online", revoke(ids[1], viewNow))

	require.EqualValues(t, 1, unrevoke(ids[0]))
	v = viewOf(t, q, ids[0])
	require.Equal(t, "pending", v.State)
	require.False(t, v.RevokedAt.Valid)
	require.Zero(t, unrevoke(ids[0]), "a satellite that is not revoked is left alone")
}

func TestMarkStaleSatellites_SQLContract(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	type seed struct {
		state, interval string
		ago             time.Duration
	}
	seeds := map[string]seed{
		"three-intervals-ago": {"online", "@every 00h00m10s", 40 * time.Second},
		"within-interval":     {"online", "@every 00h00m10s", 20 * time.Second},
		"default-threshold":   {"online", "", 20 * time.Minute},
		"within-default":      {"online", "", 10 * time.Minute},
		"floor":               {"online", "@every 00h00m01s", 5 * time.Second},
		"unparsable":          {"online", "@every 5x", 20 * time.Minute},
		"already-stale":       {"stale", "@every 00h00m10s", time.Hour},
		"revoked":             {"revoked", "@every 00h00m10s", time.Hour},
	}
	byName := map[string]int32{}
	for name, s := range seeds {
		id := createSatellites(t, q, name)[0]
		byName[name] = id
		_, err := db.ExecContext(ctx, "INSERT INTO satellite_view (satellite_id, state, heartbeat_interval, last_seen) VALUES ($1, $2, NULLIF($3, ''), $4)",
			id, s.state, s.interval, viewNow.Add(-s.ago))
		require.NoError(t, err)
	}
	createSatellites(t, q, "never-seen")

	stale, err := q.MarkStaleSatellites(ctx, database.MarkStaleSatellitesParams{
		Now: viewNow, DefaultThresholdSeconds: (15 * time.Minute).Seconds(), MinThresholdSeconds: (10 * time.Second).Seconds(),
	})
	require.NoError(t, err)

	got := map[string]time.Time{}
	for _, s := range stale {
		require.Equal(t, byName[s.Name], s.SatelliteID)
		got[s.Name] = s.LastSeen.Time
	}
	require.Len(t, got, 3, "%v", got)
	require.Contains(t, got, "unparsable", "an interval that does not parse counts as none and aborts nothing")
	require.True(t, got["three-intervals-ago"].Equal(viewNow.Add(-40*time.Second)), "stale after 3x the reported interval")
	require.Contains(t, got, "default-threshold", "STALE_THRESHOLD without an @every interval")
	for name, want := range map[string]string{
		"three-intervals-ago": "stale", "default-threshold": "stale",
		"within-interval": "online", "within-default": "online",
		"floor": "online", "already-stale": "stale", "revoked": "revoked", "unparsable": "stale",
	} {
		require.Equal(t, want, viewOf(t, q, byName[name]).State, name)
	}
	again, err := q.MarkStaleSatellites(ctx, database.MarkStaleSatellitesParams{
		Now: viewNow, DefaultThresholdSeconds: (15 * time.Minute).Seconds(), MinThresholdSeconds: (10 * time.Second).Seconds(),
	})
	require.NoError(t, err)
	require.Empty(t, again, "a satellite turns stale once")
}

func TestGetSatelliteView_PublishedGroups(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	ids := createSatellites(t, q, "edge-01", "groupless")
	for _, name := range []string{"g2", "g1"} {
		g, err := q.CreateGroup(ctx, database.CreateGroupParams{GroupName: name, RegistryUrl: "http://harbor", Projects: []string{"library"}})
		require.NoError(t, err)
		require.NoError(t, q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: ids[0], GroupID: g.ID}))
		_, err = q.RecordDesiredState(ctx, database.RecordDesiredStateParams{AggregateType: "group", AggregateID: int64(g.ID), Document: json.RawMessage(`{}`), RenderedDigest: "sha256:r-" + name})
		require.NoError(t, err)
		if name == "g1" {
			_, err = q.MarkDesiredStatePublished(ctx, database.MarkDesiredStatePublishedParams{AggregateType: "group", AggregateID: int64(g.ID), Digest: "sha256:r-g1", ArtifactDigest: "sha256:g1"})
			require.NoError(t, err)
		}
	}
	_, err := q.RecordDesiredState(ctx, database.RecordDesiredStateParams{AggregateType: "satellite", AggregateID: int64(ids[0]), Document: json.RawMessage(`{}`), RenderedDigest: "sha256:r-sat"})
	require.NoError(t, err)
	_, err = q.MarkDesiredStatePublished(ctx, database.MarkDesiredStatePublishedParams{AggregateType: "satellite", AggregateID: int64(ids[0]), Digest: "sha256:r-sat", ArtifactDigest: "sha256:sat"})
	require.NoError(t, err)

	v := viewOf(t, q, ids[0])
	require.JSONEq(t, `[{"group":"g1","published_artifact_digest":"sha256:g1"},{"group":"g2","published_artifact_digest":null}]`, string(v.PublishedGroups),
		"every group, ordered by name, in one statement")
	require.Equal(t, "pending", v.State)
	require.EqualValues(t, 1, v.DesiredVersion.Int64)
	require.Equal(t, "sha256:r-sat", v.RenderedDigest.String)
	require.Equal(t, "sha256:sat", v.PublishedArtifactDigest.String)
	require.False(t, v.PublishError.Valid)
	require.JSONEq(t, `[]`, string(v.AppliedStates))

	groupless := viewOf(t, q, ids[1])
	require.JSONEq(t, `[]`, string(groupless.PublishedGroups))
	require.False(t, groupless.DesiredVersion.Valid)

	byName, err := q.GetSatelliteViewByName(ctx, "edge-01")
	require.NoError(t, err)
	require.JSONEq(t, string(v.PublishedGroups), string(byName.PublishedGroups))
	pending, err := q.ListSatelliteViewsByState(ctx, "pending")
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.Equal(t, []string{"edge-01", "groupless"}, []string{pending[0].Name, pending[1].Name}, "ordered by id")
	online, err := q.ListSatelliteViewsByState(ctx, "online")
	require.NoError(t, err)
	require.Empty(t, online)
	_, err = q.GetSatelliteViewByID(ctx, 9999)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

// The statements read times as TIMESTAMPTZ, so an offset becomes the
// session time zone's wall clock (UTC on the test server), as NOW()
// writes the legacy columns.
func TestFlush_TimesWithOffsetKeepTheInstant(t *testing.T) {
	db := dbtest.New(t)
	q := database.New(db.DB)
	ctx := context.Background()
	id := createSatellites(t, q, "edge-01")[0]
	cest := viewNow.In(time.FixedZone("CEST", 2*60*60))

	require.NoError(t, q.FlushLastSeen(ctx, rowsJSON(t, []lastSeenRow{{ID: id, SeenAt: cest}})))
	flushView(t, q, viewRow{ID: id, SeenAt: cest, ReportedAt: cest})
	require.NoError(t, q.InsertSatelliteStatusBatch(ctx, rowsJSON(t, []statusRow{{SatelliteID: id, Activity: "a", ReportedAt: cest.Format(time.RFC3339)}})))

	var lastSeen, viewSeen, viewReported, reported time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT s.last_seen, v.last_seen, v.last_reported_at, st.reported_at
		FROM satellites s JOIN satellite_view v ON v.satellite_id = s.id JOIN satellite_status st ON st.satellite_id = s.id
		WHERE s.id = $1`, id).Scan(&lastSeen, &viewSeen, &viewReported, &reported))
	for name, got := range map[string]time.Time{
		"satellites.last_seen": lastSeen, "satellite_view.last_seen": viewSeen,
		"satellite_view.last_reported_at": viewReported, "satellite_status.reported_at": reported,
	} {
		require.True(t, got.Equal(viewNow), "%s: %s", name, got)
	}
}
