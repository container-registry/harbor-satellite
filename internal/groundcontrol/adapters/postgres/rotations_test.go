package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
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

const pendingRotation = "the postgres rotation store is a stub; robots are not rotated"

// expiringRobot is satellite edge-01 with robot 42 expiring in a day.
func expiringRobot(t *testing.T, db *dbtest.DB) fleet.SatelliteID {
	t.Helper()
	q := database.New(db.DB)
	sat, err := q.CreateSatellite(context.Background(), "edge-01")
	require.NoError(t, err)
	_, err = q.AddRobotAccount(context.Background(), database.AddRobotAccountParams{
		RobotName: "robot$edge-01-1", RobotSecretHash: "old-hash", RobotID: "42", SatelliteID: sat.ID,
		RobotExpiry: sql.NullTime{Time: hotNow.Add(24 * time.Hour), Valid: true},
	})
	require.NoError(t, err)
	hash := sha256.Sum256([]byte("hss_edge-01"))
	require.NoError(t, q.IssueSatelliteSession(context.Background(), database.IssueSatelliteSessionParams{
		SatelliteID: sat.ID, TokenHash: hash[:], ExpiresAt: hotNow.Add(time.Hour),
	}))
	return fleet.SatelliteID(sat.ID)
}

func TestRotationStore_RoundTrip(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	db := dbtest.New(t)
	ctx := context.Background()
	id := expiringRobot(t, db)
	store, err := postgres.NewRotationStore(db.DB)
	require.NoError(t, err)

	robots, err := store.ExpiringBefore(ctx, hotNow, hotNow.Add(72*time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, robots, 1)
	require.Equal(t, int64(42), robots[0].HarborID)
	rotation := fleet.RobotRotation{
		Satellite: id, Generation: 1, Previous: robots[0],
		Successor: fleet.RobotCredentials{HarborID: 43, Name: fleet.SuccessorRobotName(id, "edge-01", 1), ExpiresAt: hotNow.Add(30 * 24 * time.Hour)},
	}
	gen, err := store.Stage(ctx, rotation, []byte("sealed"), "new-hash")
	require.NoError(t, err)
	require.Equal(t, int64(1), gen)

	pendingRotations, err := store.Pending(ctx)
	require.NoError(t, err)
	require.Len(t, pendingRotations, 1)
	require.Equal(t, []byte("sealed"), pendingRotations[0].SealedSecret)
	acked, err := store.Acked(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, acked, "not acked yet")

	_, err = db.ExecContext(ctx, "UPDATE robot_rotations SET acked_at = NOW()")
	require.NoError(t, err)
	acked, err = store.Acked(ctx, 10)
	require.NoError(t, err)
	require.Len(t, acked, 1)
	require.Equal(t, int64(42), acked[0].Previous.HarborID)
	require.NoError(t, store.Promote(ctx, acked[0]))

	var kind string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT kind FROM event_outbox ORDER BY id DESC LIMIT 1").Scan(&kind))
	require.Equal(t, string(fleet.EventRobotRotated), kind, "Promote appends RobotRotated in its transaction")
	robot, err := database.New(db.DB).GetRobotAccBySatelliteID(ctx, int32(id))
	require.NoError(t, err)
	require.Equal(t, "43", robot.RobotID)
}

func TestHeartbeatStore_AcksRotationOnlyWhenPresent(t *testing.T) {
	pending.Plan(t, "8.4", pendingRotation)
	db := dbtest.New(t)
	ctx := context.Background()
	id := expiringRobot(t, db)
	_, err := database.New(db.DB).StageRobotRotation(ctx, database.StageRobotRotationParams{
		SatelliteID: int32(id), Generation: 1, RobotName: "robot$edge-01-1-g1", RobotHarborID: 43,
		SecretCiphertext: []byte("sealed"), SecretHash: "new-hash",
	})
	require.NoError(t, err)
	store, err := postgres.NewHeartbeatStore(db.DB, fleettest.Clock{T: hotNow})
	require.NoError(t, err)
	flush := func(generation int64) int64 {
		t.Helper()
		batch := heartbeat.Batch{
			Latest:  []fleet.Heartbeat{{Satellite: id, SeenAt: hotNow, Interval: "@every 00h00m30s", CredentialsGeneration: generation}},
			Samples: []fleet.StatusSample{{Satellite: id, Activity: "idle", ReportedAt: hotNow}},
		}
		used := db.MeasureStatements(t, func() { require.NoError(t, store.Flush(ctx, batch)) })
		return used.Calls["AckRobotRotations"]
	}

	require.Zero(t, flush(0), "no ack, no statement")
	require.EqualValues(t, 1, flush(1))
	var acked bool
	require.NoError(t, db.QueryRowContext(ctx, "SELECT acked_at IS NOT NULL FROM robot_rotations").Scan(&acked))
	require.True(t, acked)
}
