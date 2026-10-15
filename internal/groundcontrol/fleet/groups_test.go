package fleet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

// seedGroup is group g1 with members 7 and 8 and group g2.
func seedGroup(w *world) {
	w.repo.Groups.ByName = map[string]fleet.Group{
		"g1": {ID: 1, Name: "g1", Projects: []string{"library"}},
		"g2": {ID: 2, Name: "g2", Projects: []string{"team"}},
	}
	w.repo.Groups.MembersOf = map[fleet.GroupID][]fleet.SatelliteID{1: {7, 8}}
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}, "edge-02": {ID: 8, Name: "edge-02"}}
}

func TestGroupService_Sync(t *testing.T) {
	cmd := fleet.SyncGroupCmd{Name: "g1", Artifacts: []fleet.Artifact{
		{Repository: "library/alpine", Tags: []string{"3.20"}, Type: "image"},
		{Repository: "team/app", Tags: []string{"v1"}, Type: "image"},
		{Repository: "library/busybox", Tags: []string{"1.36"}, Type: "image"},
		{Repository: "gone/old", Tags: []string{"v0"}, Type: "image", Deleted: true},
	}}

	t.Run("stores the group with GroupChanged, nothing per member", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedGroup(w)

		group, err := w.services(t).Groups.Sync(context.Background(), cmd)

		require.NoError(t, err)
		require.Equal(t, fleet.GroupID(1), group.ID)
		stored := arg[fleet.Group](t, onlyCall(t, w.rec, "Groups.Upsert"), 0)
		require.Equal(t, []string{"library", "team"}, stored.Projects, "distinct projects of the artifacts that are not deleted")
		require.Equal(t, cmd.Artifacts, stored.Artifacts)
		require.Equal(t, []fleet.Event{{
			Kind:      fleet.EventGroupChanged,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateGroup, ID: 1},
			Payload:   fleet.ChangedPayload{Name: "g1"},
		}}, w.repo.Events.Appended())
		insideTx(t, w.rec, "Events.Append")
		noRegistryCalls(t, w.rec)
		require.Zero(t, w.rec.Count("Groups.Members"), "the group reconciler fans out to the members")
		require.Zero(t, w.rec.Count("Satellites.RobotGrants"))
	})

	t.Run("a failure leaves the group and the outbox unchanged", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedGroup(w)
		boom := errors.New("database failed")
		w.rec.FailOn("Events.Append", boom)

		_, err := w.services(t).Groups.Sync(context.Background(), cmd)

		require.ErrorIs(t, err, boom)
		require.Zero(t, w.rec.Count("Commit"))
	})
}

func TestGroupService_DeleteAndMembership(t *testing.T) {
	assignment := func(satellite fleet.SatelliteID, name string) fleet.Event {
		return fleet.Event{
			Kind:      fleet.EventAssignmentChanged,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: int64(satellite)},
			Payload:   fleet.ChangedPayload{Name: name},
		}
	}

	t.Run("adding a satellite is AssignmentChanged", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedGroup(w)

		require.NoError(t, w.services(t).Groups.AddSatellite(context.Background(), fleet.MembershipCmd{Satellite: "edge-01", Group: "g2"}))

		add := onlyCall(t, w.rec, "Groups.AddMember")
		require.Equal(t, fleet.GroupID(2), arg[fleet.GroupID](t, add, 0))
		require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, add, 1))
		require.Equal(t, []fleet.Event{assignment(7, "edge-01")}, w.repo.Events.Appended())
		noRegistryCalls(t, w.rec)
	})

	t.Run("removing a satellite is AssignmentChanged, its last group too (D10)", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedGroup(w)

		require.NoError(t, w.services(t).Groups.RemoveSatellite(context.Background(), fleet.MembershipCmd{Satellite: "edge-01", Group: "g1"}))

		onlyCall(t, w.rec, "Groups.RemoveMember")
		require.Equal(t, []fleet.Event{assignment(7, "edge-01")}, w.repo.Events.Appended())
		noRegistryCalls(t, w.rec)
	})

	t.Run("deleting a group appends a tombstone naming its members", func(t *testing.T) {
		pending.Plan(t, "2.5", pendingServices)
		w := newWorld()
		seedGroup(w)

		require.NoError(t, w.services(t).Groups.Delete(context.Background(), "g1"))

		require.Equal(t, fleet.GroupID(1), arg[fleet.GroupID](t, onlyCall(t, w.rec, "Groups.Delete"), 0))
		require.Equal(t, []fleet.Event{{
			Kind:      fleet.EventGroupDeleted,
			Aggregate: fleet.Aggregate{Type: fleet.AggregateGroup, ID: 1},
			Payload:   fleet.GroupDeletedPayload{Name: "g1", Members: []fleet.SatelliteID{7, 8}, StateRef: stateRef("group", "g1")},
		}}, w.repo.Events.Appended())
		insideTx(t, w.rec, "Groups.Members")
		noRegistryCalls(t, w.rec)
	})
}
