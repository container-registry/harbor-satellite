package fleet_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestArtifactRef_String(t *testing.T) {
	ref := fleet.ArtifactRef{Registry: "https://harbor.example", Repository: "satellite/group-state/g1/state", Tag: "latest"}

	require.Equal(t, "https://harbor.example/satellite/group-state/g1/state:latest", ref.String())
}

// The references must stay the strings utils.Assemble*State builds today,
// because satellites already store them.
func TestSettings_StateRefsMatchTodaysURLs(t *testing.T) {
	pending.Plan(t, "1.2", "state references are built from env.GC in utils/helper.go")
	s := fleet.Settings{RegistryURL: "http://harbor.example:8080"}

	for _, tc := range []struct {
		ref  func(string) (fleet.ArtifactRef, error)
		want string
	}{
		{s.GroupStateRef, "http://harbor.example:8080/satellite/group-state/edge.group-1/state:latest"},
		{s.ConfigStateRef, "http://harbor.example:8080/satellite/config-state/edge.group-1/state:latest"},
		{s.SatelliteStateRef, "http://harbor.example:8080/satellite/satellite-state/edge.group-1/state:latest"},
	} {
		ref, err := tc.ref("edge.group-1")
		require.NoError(t, err)
		require.Equal(t, tc.want, ref.String())
		require.Equal(t, "latest", ref.Tag)
	}
}

func TestSettings_StateRefRejectsInvalidNames(t *testing.T) {
	pending.Plan(t, "1.2", "state references are built from env.GC in utils/helper.go")
	s := fleet.Settings{RegistryURL: "http://harbor.example"}

	for _, name := range []string{"", "Edge", "a/b", "../x"} {
		_, err := s.GroupStateRef(name)
		require.ErrorIs(t, err, fleet.ErrInvalid, "name %q", name)
	}
}
