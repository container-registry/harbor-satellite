package fleet_test

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

func TestGroupService_FollowPushMovesTheTagsDigest(t *testing.T) {
	w := newWorld()
	w.repo.Groups.ByName = map[string]fleet.Group{
		"g1": {ID: 1, Name: "g1", Artifacts: []fleet.Artifact{{Repository: "library/alpine", Tags: []string{"3.20"}, Digest: "sha256:old"}}},
		"g2": {ID: 2, Name: "g2", Artifacts: []fleet.Artifact{{Repository: "library/alpine", Tags: []string{"3.19"}, Digest: "sha256:old"}}},
	}

	updated, err := w.services(t).Groups.FollowPush(context.Background(), fleet.RegistryEvent{
		Type: fleet.RegistryArtifactPushed, Project: "library", Repository: "alpine", Tag: "3.20", Digest: "sha256:new",
	})

	require.NoError(t, err)
	require.Equal(t, []string{"g1"}, updated, "only the group that lists the pushed tag")
	upsert := onlyCall(t, w.rec, "Groups.Upsert")
	stored := arg[fleet.Group](t, upsert, 0)
	require.Equal(t, "sha256:new", stored.Artifacts[0].Digest)
	require.Equal(t, []string{"library"}, stored.Projects)
	insideTx(t, w.rec, "Groups.Upsert")
	events := w.repo.Events.Appended()
	require.Len(t, events, 1)
	require.Equal(t, fleet.EventGroupChanged, events[0].Kind)
}

func TestGroupService_FollowPushIgnoresOtherEventsAndSameDigest(t *testing.T) {
	w := newWorld()
	w.repo.Groups.ByName = map[string]fleet.Group{
		"g1": {ID: 1, Name: "g1", Artifacts: []fleet.Artifact{{Repository: "library/alpine", Tags: []string{"3.20"}, Digest: "sha256:new"}}},
	}
	svc := w.services(t)

	for _, e := range []fleet.RegistryEvent{
		{Type: fleet.RegistryArtifactPushed, Repository: "library/alpine", Tag: "3.20", Digest: "sha256:new"},
		{Type: "ArtifactDeleted", Repository: "library/alpine", Tag: "3.20", Digest: "sha256:other"},
		{Type: fleet.RegistryArtifactPushed, Repository: "library/alpine", Digest: "sha256:untagged"},
	} {
		updated, err := svc.Groups.FollowPush(context.Background(), e)
		require.NoError(t, err)
		require.Empty(t, updated, "%+v", e)
	}
	require.Zero(t, w.rec.Count("Groups.Upsert"))
}
