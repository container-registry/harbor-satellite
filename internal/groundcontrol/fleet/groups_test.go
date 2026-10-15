package fleet_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// seedGroup is group g1 with members 7 (also in another group with "team")
// and 8, and their robots.
func seedGroup(w *world) {
	w.repo.Groups.ByName = map[string]fleet.Group{"g1": {ID: 1, Name: "g1", Projects: []string{"library"}}}
	w.repo.Groups.MembersOf = map[fleet.GroupID][]fleet.SatelliteID{1: {7, 8}}
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}, "edge-02": {ID: 8, Name: "edge-02"}}
	w.repo.Satellites.GrantsOf = map[fleet.SatelliteID]fleet.RobotGrant{
		7: grant(7, 42, "library", "team"),
		8: grant(8, 43, "library"),
	}
	w.repo.Robots.BySatelliteID = map[fleet.SatelliteID]fleet.Robot{
		7: {ID: 5, Satellite: 7, HarborID: 42},
		8: {ID: 6, Satellite: 8, HarborID: 43},
	}
}

func grant(satellite fleet.SatelliteID, harborID int64, projects ...string) fleet.RobotGrant {
	return fleet.RobotGrant{Satellite: satellite, HarborID: harborID, Projects: projects}
}

func TestGroupService_Sync(t *testing.T) {
	cmd := fleet.SyncGroupCmd{Name: "g1", Artifacts: []fleet.Artifact{
		{Repository: "library/alpine", Tags: []string{"3.20"}, Type: "image"},
		{Repository: "team/app", Tags: []string{"v1"}, Type: "image"},
		{Repository: "library/busybox", Tags: []string{"1.36"}, Type: "image"},
		{Repository: "gone/old", Tags: []string{"v0"}, Type: "image", Deleted: true},
	}}

	t.Run("stores projects, pushes the group state and grants each member its union", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedGroup(w)

		group, err := w.services(t).Groups.Sync(context.Background(), cmd)

		require.NoError(t, err)
		require.Equal(t, fleet.GroupID(1), group.ID)
		stored := arg[fleet.Group](t, onlyCall(t, w.rec, "Groups.Upsert"), 0)
		require.Equal(t, []string{"library", "team"}, stored.Projects)
		require.Equal(t, cmd.Artifacts, stored.Artifacts)
		push := onlyCall(t, w.rec, "PushState")
		require.Equal(t, stateRef("group", "g1"), arg[fleet.ArtifactRef](t, push, 0))
		var doc struct {
			Artifacts []fleet.Artifact `json:"artifacts"`
			Group     string           `json:"group"`
			Registry  string           `json:"registry"`
		}
		require.NoError(t, json.Unmarshal(arg[[]byte](t, push, 1), &doc))
		require.Equal(t, "g1", doc.Group)
		require.Equal(t, registryURL, doc.Registry)
		require.Len(t, doc.Artifacts, 4)
		granted := map[int64][]string{}
		for _, c := range w.rec.CallsTo("SetRobotProjects") {
			granted[arg[int64](t, c, 0)] = arg[[]string](t, c, 1)
		}
		require.Len(t, granted, 2, "one update per member")
		require.ElementsMatch(t, []string{"library", "team", fleet.SatelliteProject}, granted[42])
		require.ElementsMatch(t, []string{"library", fleet.SatelliteProject}, granted[43])
		lookup := onlyCall(t, w.rec, "Satellites.RobotGrants")
		require.ElementsMatch(t, []fleet.SatelliteID{7, 8}, arg[[]fleet.SatelliteID](t, lookup, 0), "one lookup for all members")
		require.Zero(t, w.rec.Count("Robots.BySatellite"), "no lookup per member")
		require.Equal(t, 1, w.rec.Count("Commit"))
	})

	for _, step := range []string{"Groups.Upsert", "PushState", "SetRobotProjects"} {
		t.Run("leaves the group unchanged when "+step+" fails", func(t *testing.T) {
			pending.Plan(t, "1.2", pendingServices)
			w := newWorld()
			seedGroup(w)
			boom := errors.New("step failed")
			w.rec.FailOn(step, boom)

			_, err := w.services(t).Groups.Sync(context.Background(), cmd)

			require.ErrorIs(t, err, boom)
			require.Zero(t, w.rec.Count("Commit"))
		})
	}
}

func TestGroupService_DeleteAndMembership(t *testing.T) {
	t.Run("adding a satellite grants the union of all its groups once", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedGroup(w)
		g2 := fleet.Group{ID: 2, Name: "g2", Projects: []string{"team"}}
		w.repo.Groups.ByName["g2"] = g2
		w.repo.Satellites.GroupsOf = map[fleet.SatelliteID][]fleet.Group{7: {w.repo.Groups.ByName["g1"], g2}}
		w.repo.Satellites.ConfigOf = map[fleet.SatelliteID]fleet.Config{7: {ID: 3, Name: "edge-config"}}

		require.NoError(t, w.services(t).Groups.AddSatellite(context.Background(), fleet.MembershipCmd{Satellite: "edge-01", Group: "g2"}))

		add := onlyCall(t, w.rec, "Groups.AddMember")
		require.Equal(t, fleet.GroupID(2), arg[fleet.GroupID](t, add, 0))
		require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, add, 1))
		set := onlyCall(t, w.rec, "SetRobotProjects")
		require.Equal(t, int64(42), arg[int64](t, set, 0))
		require.ElementsMatch(t, []string{"library", "team", fleet.SatelliteProject}, arg[[]string](t, set, 1))
		push := onlyCall(t, w.rec, "PushState")
		require.Equal(t, stateRef("satellite", "edge-01"), arg[fleet.ArtifactRef](t, push, 0))
		states, _ := pushedSatelliteState(t, push)
		require.Equal(t, []string{stateRef("group", "g1").String(), stateRef("group", "g2").String()}, states)
	})

	t.Run("removing the last group publishes a state without groups (D10)", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedGroup(w)
		w.repo.Satellites.GrantsOf[7] = grant(7, 42)
		w.repo.Satellites.ConfigOf = map[fleet.SatelliteID]fleet.Config{7: {ID: 3, Name: "edge-config"}}

		require.NoError(t, w.services(t).Groups.RemoveSatellite(context.Background(), fleet.MembershipCmd{Satellite: "edge-01", Group: "g1"}))

		onlyCall(t, w.rec, "Groups.RemoveMember")
		set := onlyCall(t, w.rec, "SetRobotProjects")
		require.Equal(t, []string{fleet.SatelliteProject}, arg[[]string](t, set, 1))
		states, config := pushedSatelliteState(t, onlyCall(t, w.rec, "PushState"))
		require.Empty(t, states)
		require.Equal(t, stateRef("config", "edge-config").String(), config)
	})

	t.Run("deleting a group deletes its state and regrants every member", func(t *testing.T) {
		pending.Plan(t, "1.2", pendingServices)
		w := newWorld()
		seedGroup(w)
		w.repo.Satellites.GrantsOf = map[fleet.SatelliteID]fleet.RobotGrant{7: grant(7, 42, "team"), 8: grant(8, 43)}

		require.NoError(t, w.services(t).Groups.Delete(context.Background(), "g1"))

		require.Equal(t, fleet.GroupID(1), arg[fleet.GroupID](t, onlyCall(t, w.rec, "Groups.Delete"), 0))
		require.Equal(t, stateRef("group", "g1"), arg[fleet.ArtifactRef](t, onlyCall(t, w.rec, "DeleteState"), 0))
		granted := map[int64][]string{}
		for _, c := range w.rec.CallsTo("SetRobotProjects") {
			granted[arg[int64](t, c, 0)] = arg[[]string](t, c, 1)
		}
		require.ElementsMatch(t, []string{"team", fleet.SatelliteProject}, granted[42])
		require.ElementsMatch(t, []string{fleet.SatelliteProject}, granted[43])
		require.Equal(t, 2, w.rec.Count("PushState"), "each member's state drops the group")
	})
}
