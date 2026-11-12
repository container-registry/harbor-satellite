package postgres_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/adapters/postgres"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbtest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

// Ground Control before 1.0 named a robot "robot$satellite-<satellite>", so
// a satellite named like a successor (edge-<id>-g3) carries a robot name
// that reads as generation 3 although it never rotated. Its first rotation
// is generation 1, as robot_rotations says.
func TestRobotRotator_FirstRotationOfALegacyRobotIsGenerationOne(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	q := database.New(db.DB)
	placeholder, err := q.CreateSatellite(ctx, "placeholder")
	require.NoError(t, err)
	name := fmt.Sprintf("edge-%d-g3", placeholder.ID+1)
	sat, err := q.CreateSatellite(ctx, name)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("edge-%d-g3", sat.ID), name)
	_, err = q.AddRobotAccount(ctx, database.AddRobotAccountParams{
		RobotName: "robot$satellite-" + name, RobotSecretHash: "old-hash", RobotID: "42", SatelliteID: sat.ID,
		RobotExpiry: sql.NullTime{Time: time.Now().Add(24 * time.Hour), Valid: true},
	})
	require.NoError(t, err)
	hash := sha256.Sum256([]byte("hss_legacy"))
	require.NoError(t, q.IssueSatelliteSession(ctx, database.IssueSatelliteSessionParams{SatelliteID: sat.ID, TokenHash: hash[:], ExpiresAt: time.Now().Add(time.Hour)}))

	repo, err := postgres.NewRepository(db.DB)
	require.NoError(t, err)
	store, err := postgres.NewRotationStore(db.DB)
	require.NoError(t, err)
	locker, err := postgres.NewAggregateLocker(ctx, db.DB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = locker.Close() })
	registry := fleettest.NewRegistry(nil)
	registry.Credentials = fleet.RobotCredentials{HarborID: 43, Secret: "successor", ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
	rotator, err := async.NewRobotRotator(store, repo, registry, locker, plainSealer{}, fleet.SystemClock{}, 30*24*time.Hour, 72*time.Hour, time.Minute)
	require.NoError(t, err)

	staged, _, err := rotator.RunOnce(ctx)

	require.NoError(t, err)
	require.Equal(t, 1, staged)
	var generation int64
	var robotName string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT generation, robot_name FROM robot_rotations WHERE satellite_id = $1", sat.ID).Scan(&generation, &robotName))
	require.Equal(t, int64(1), generation)
	require.Equal(t, fleet.SuccessorRobotName(fleet.SatelliteID(sat.ID), name, 1), robotName)
}
