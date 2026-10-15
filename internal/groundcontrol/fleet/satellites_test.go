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

// Plan item 2.5: a request is one transaction plus its events; the async
// plane does the registry work.
const pendingServices = "fleet services are stubs; handlers call Harbor in the request"

// seedRegistration is a config and two groups.
func seedRegistration(w *world) {
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 3, Name: "edge-config"}}
	w.repo.Groups.ByName = map[string]fleet.Group{
		"g1": {ID: 1, Name: "g1", Projects: []string{"library"}},
		"g2": {ID: 2, Name: "g2", Projects: []string{"library", "team"}},
	}
	w.repo.Satellites.NextID = 7
}

func TestSatelliteService_Register(t *testing.T) {
	cmd := fleet.RegisterSatelliteCmd{Name: "edge-01", ConfigName: "edge-config", Groups: []string{"g1", "g2"}}

	t.Run("one transaction with SatelliteCreated and no registry call", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedRegistration(w)

		tok, err := w.services(t).Satellites.Register(context.Background(), cmd)

		require.NoError(t, err)
		require.NotEmpty(t, tok.Token)
		require.Equal(t, now.Add(fleet.DefaultTokenTTL), tok.ExpiresAt)
		noRegistryCalls(t, w.rec)
		require.Equal(t, "edge-01", arg[string](t, onlyCall(t, w.rec, "Satellites.Create"), 0))
		var members []fleet.GroupID
		for _, c := range w.rec.CallsTo("Groups.AddMember") {
			require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, c, 1))
			members = append(members, arg[fleet.GroupID](t, c, 0))
		}
		require.ElementsMatch(t, []fleet.GroupID{1, 2}, members)
		set := onlyCall(t, w.rec, "Satellites.SetConfig")
		require.Equal(t, fleet.ConfigID(3), arg[fleet.ConfigID](t, set, 1))
		add := onlyCall(t, w.rec, "Tokens.Add")
		require.Equal(t, tok.Token, arg[string](t, add, 1))
		require.Equal(t, tok.ExpiresAt, arg[time.Time](t, add, 2))
		require.Equal(t, []fleet.Event{{
			Kind:      fleet.EventSatelliteCreated,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7},
			Payload:   fleet.ChangedPayload{Name: "edge-01"},
		}}, w.repo.Events.Appended())
		for _, m := range []string{"Satellites.Create", "Tokens.Add", "Events.Append"} {
			insideTx(t, w.rec, m)
		}
		require.Equal(t, 1, w.rec.Count("Commit"))
	})

	t.Run("a failure rolls back the event too", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedRegistration(w)
		boom := errors.New("database failed")
		w.rec.FailOn("Tokens.Add", boom)

		_, err := w.services(t).Satellites.Register(context.Background(), cmd)

		require.ErrorIs(t, err, boom)
		require.Zero(t, w.rec.Count("Commit"))
		require.Equal(t, 1, w.rec.Count("Rollback"))
	})

	t.Run("existing satellite is ErrAlreadyExists", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedRegistration(w)
		w.rec.FailOn("Satellites.Create", fleet.ErrAlreadyExists)

		_, err := w.services(t).Satellites.Register(context.Background(), cmd)

		require.ErrorIs(t, err, fleet.ErrAlreadyExists)
		require.Zero(t, w.rec.Count("Commit"))
		noRegistryCalls(t, w.rec)
	})

	t.Run("groupless satellite is created all the same (D10)", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedRegistration(w)

		_, err := w.services(t).Satellites.Register(context.Background(), fleet.RegisterSatelliteCmd{Name: "edge-01", ConfigName: "edge-config"})

		require.NoError(t, err)
		require.Zero(t, w.rec.Count("Groups.AddMember"))
		require.Len(t, w.repo.Events.Appended(), 1, "the reconciler publishes the empty state")
	})
}

func TestSatelliteService_DeleteAppendsATombstone(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()
	spiffeID := "spiffe://harbor-satellite.local/satellite/region/default/edge-01"
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01", SPIFFEID: spiffeID}}
	w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{7: {ID: 5, Satellite: 7, HarborID: 42}}

	require.NoError(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"))

	noRegistryCalls(t, w.rec)
	require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, onlyCall(t, w.rec, "Satellites.Delete"), 0))
	require.Equal(t, []fleet.Event{{
		Kind:      fleet.EventSatelliteDeleted,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7},
		Payload: fleet.SatelliteDeletedPayload{
			Name: "edge-01", RobotHarborIDs: []int64{42}, SPIFFEID: spiffeID, StateRef: stateRef("satellite", "edge-01"),
		},
	}}, w.repo.Events.Appended())
	insideTx(t, w.rec, "Events.Append")
	require.Equal(t, 1, w.rec.Count("Commit"))
}

func TestSatelliteService_UnknownIsErrNotFound(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()

	_, err := w.services(t).Satellites.Get(context.Background(), "edge-01")
	require.ErrorIs(t, err, fleet.ErrNotFound)
	require.ErrorIs(t, w.services(t).Satellites.Delete(context.Background(), "edge-01"), fleet.ErrNotFound)
	require.Empty(t, w.repo.Events.Appended())
}
