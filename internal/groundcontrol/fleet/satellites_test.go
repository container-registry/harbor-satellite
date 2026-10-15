package fleet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingServices = "fleet services are stubs; handlers still call Harbor and sqlc directly"

// seedRegistration is a config, two groups sharing the project "library"
// and a registry that hands out robot 42.
func seedRegistration(w *world) {
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 3, Name: "edge-config"}}
	w.repo.Groups.ByName = map[string]fleet.Group{
		"g1": {ID: 1, Name: "g1", Projects: []string{"library"}},
		"g2": {ID: 2, Name: "g2", Projects: []string{"library", "team"}},
	}
	w.repo.Satellites.NextID = 7
	w.repo.Satellites.GrantsOf = map[fleet.SatelliteID]fleet.RobotGrant{
		7: {Satellite: 7, HarborID: 42, RobotName: "robot$edge-01", Projects: []string{"library", "library", "team"}},
	}
	w.registry.Credentials = fleet.RobotCredentials{HarborID: 42, Name: "robot$edge-01", Secret: "s3cret", ExpiresAt: now.Add(30 * 24 * time.Hour)}
}

func TestSatelliteService_Register(t *testing.T) {
	cmd := fleet.RegisterSatelliteCmd{Name: "edge-01", ConfigName: "edge-config", Groups: []string{"g1", "g2"}}

	t.Run("creates the robot with the union of its groups and publishes the state", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedRegistration(w)

		tok, err := w.services(t).Satellites.Register(context.Background(), cmd)

		require.NoError(t, err)
		require.NotEmpty(t, tok.Token)
		require.Equal(t, now.Add(fleet.DefaultTokenTTL), tok.ExpiresAt)
		spec := arg[fleet.RobotSpec](t, onlyCall(t, w.rec, "CreateRobot"), 0)
		require.Equal(t, "edge-01", spec.Name)
		require.Equal(t, 30*24*time.Hour, spec.Duration)
		require.ElementsMatch(t, []string{"library", "team", fleet.SatelliteProject}, grantOf(t, w.rec, 42))
		require.LessOrEqual(t, w.rec.Count("SetRobotProjects"), 1, "at most one update after creating the robot")
		push := onlyCall(t, w.rec, "PushState")
		require.Equal(t, stateRef("satellite", "edge-01"), arg[fleet.ArtifactRef](t, push, 0))
		states, config := pushedSatelliteState(t, push)
		require.Equal(t, []string{stateRef("group", "g1").String(), stateRef("group", "g2").String()}, states)
		require.Equal(t, stateRef("config", "edge-config").String(), config)
		robot := arg[fleet.Robot](t, onlyCall(t, w.rec, "Robots.Add"), 0)
		require.Equal(t, fleet.SatelliteID(7), robot.Satellite)
		require.Equal(t, int64(42), robot.HarborID)
		require.NotEmpty(t, robot.SecretHash)
		require.NotEqual(t, "s3cret", robot.SecretHash)
		add := onlyCall(t, w.rec, "Tokens.Add")
		require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, add, 0))
		require.Equal(t, tok.Token, arg[string](t, add, 1))
		require.Equal(t, tok.ExpiresAt, arg[time.Time](t, add, 2))
		require.Equal(t, 1, w.rec.Count("Commit"))
		require.Zero(t, w.rec.Count("DeleteRobot"))
	})

	for _, step := range []string{"CreateRobot", "PushState"} {
		t.Run("rolls back when "+step+" fails", func(t *testing.T) {
			pending.Plan(t, "1.2", pendingServices)
			w := newWorld()
			seedRegistration(w)
			boom := errors.New("registry failed")
			w.rec.FailOn(step, boom)

			_, err := w.services(t).Satellites.Register(context.Background(), cmd)

			require.ErrorIs(t, err, boom)
			require.Zero(t, w.rec.Count("Commit"))
			if step == "PushState" {
				require.Equal(t, int64(42), arg[int64](t, onlyCall(t, w.rec, "DeleteRobot"), 0))
			} else {
				require.Zero(t, w.rec.Count("DeleteRobot"))
			}
		})
	}

	t.Run("existing robot is ErrAlreadyExists", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedRegistration(w)
		w.registry.Robots = map[string]fleet.RobotCredentials{"edge-01": {HarborID: 41, Name: "robot$edge-01"}}

		_, err := w.services(t).Satellites.Register(context.Background(), cmd)

		require.ErrorIs(t, err, fleet.ErrAlreadyExists)
		require.Zero(t, w.rec.Count("CreateRobot"))
		require.Zero(t, w.rec.Count("Commit"))
	})

	t.Run("existing satellite is ErrAlreadyExists", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedRegistration(w)
		w.rec.FailOn("Satellites.Create", fleet.ErrAlreadyExists)

		_, err := w.services(t).Satellites.Register(context.Background(), cmd)

		require.ErrorIs(t, err, fleet.ErrAlreadyExists)
		require.Zero(t, w.rec.Count("CreateRobot"))
		require.Zero(t, w.rec.Count("Commit"))
	})

	t.Run("groupless satellite still publishes its state (D10)", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedRegistration(w)
		w.repo.Satellites.GrantsOf = map[fleet.SatelliteID]fleet.RobotGrant{7: {Satellite: 7, HarborID: 42, RobotName: "robot$edge-01"}}

		_, err := w.services(t).Satellites.Register(context.Background(), fleet.RegisterSatelliteCmd{Name: "edge-01", ConfigName: "edge-config"})

		require.NoError(t, err)
		push := onlyCall(t, w.rec, "PushState")
		require.Equal(t, stateRef("satellite", "edge-01"), arg[fleet.ArtifactRef](t, push, 0))
		states, config := pushedSatelliteState(t, push)
		require.Empty(t, states)
		require.Equal(t, stateRef("config", "edge-config").String(), config)
	})
}

func TestSatelliteService_DeleteRemovesRobotAndState(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, HarborID: 42}}

	require.NoError(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"))

	require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, onlyCall(t, w.rec, "Satellites.Delete"), 0))
	require.Equal(t, int64(42), arg[int64](t, onlyCall(t, w.rec, "DeleteRobot"), 0))
	require.Equal(t, stateRef("satellite", "edge-01"), arg[fleet.ArtifactRef](t, onlyCall(t, w.rec, "DeleteState"), 0))
	require.Equal(t, 1, w.rec.Count("Commit"))
}

func TestSatelliteService_UnknownIsErrNotFound(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()

	_, err := w.services(t).Satellites.Get(context.Background(), "edge-01")
	require.ErrorIs(t, err, fleet.ErrNotFound)
	require.ErrorIs(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"), fleet.ErrNotFound)
	require.Zero(t, w.rec.Count("DeleteRobot"))
}
