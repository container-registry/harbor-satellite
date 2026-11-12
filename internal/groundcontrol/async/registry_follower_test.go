package async_test

import (
	"context"
	"errors"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

type followGroups struct{ seen []fleet.RegistryEvent }

func (g *followGroups) FollowPush(_ context.Context, e fleet.RegistryEvent) ([]string, error) {
	g.seen = append(g.seen, e)
	if e.Tag == "broken" {
		return nil, errors.New("store down")
	}
	return []string{"g1"}, nil
}

func TestRegistryFollower_HandsEveryEventToTheGroups(t *testing.T) {
	src := &fleettest.RegistryEventSource{Events: []fleet.RegistryEvent{
		{Type: fleet.RegistryArtifactPushed, Repository: "library/alpine", Tag: "3.20", Digest: "sha256:a"},
		{Type: fleet.RegistryArtifactPushed, Repository: "library/alpine", Tag: "broken", Digest: "sha256:b"},
		{Type: fleet.RegistryArtifactPushed, Repository: "library/alpine", Tag: "3.21", Digest: "sha256:c"},
	}}
	groups := &followGroups{}
	f, err := async.NewRegistryFollower(src, groups)
	require.NoError(t, err)
	require.Equal(t, "registry-follower", f.Name())

	require.NoError(t, f.Run(context.Background()))

	require.Len(t, groups.seen, 3, "a failed event does not stop the subscription")
	require.NoError(t, src.Errors[0])
	require.Error(t, src.Errors[1])
	require.NoError(t, src.Errors[2])
}
