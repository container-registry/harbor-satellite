package fleet_test

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestRobotName_UniquePerSatelliteID(t *testing.T) {
	require.Equal(t, "edge-01-7", fleet.RobotName(7, "edge-01"))
	require.NotEqual(t, fleet.RobotName(7, "edge-01"), fleet.RobotName(9, "edge-01"), "a re-created satellite gets another robot")
	require.NotEqual(t, fleet.RobotName(23, "x-1"), fleet.RobotName(123, "x"), "the ID after the last dash keeps names apart")
}

// goldenDesiredSatellite is what the satellite reconciler records for a
// satellite in g1 and g2 whose robot may pull library: the state document
// exactly as server.SatelliteStateArtifact encodes it, and the robot's
// projects, so a change of either is published (A87).
const goldenDesiredSatellite = `{"robot_projects":["library","satellite"],"state":{"config":"http://harbor.example:8080/satellite/config-state/edge-config/state:latest","states":["http://harbor.example:8080/satellite/group-state/g1/state:latest","http://harbor.example:8080/satellite/group-state/g2/state:latest"]}}`

const goldenDesiredSatelliteDigest = fleet.Digest("sha256:ea7f03f214248c199e9325dbbced6ccec1695f08b612d6fa14bd2b96ce0a0055")

func TestDesiredSatelliteGoldenDigest(t *testing.T) {
	sum := sha256.Sum256([]byte(goldenDesiredSatellite))
	require.Equal(t, goldenDesiredSatelliteDigest, fleet.Digest("sha256:"+hex.EncodeToString(sum[:])))
}

func TestRenderSatelliteDesiredState_MatchesGolden(t *testing.T) {
	pending.Plan(t, "2.3", "nothing renders desired state; handlers push artifacts directly")
	ref := func(kind, name string) fleet.ArtifactRef {
		return fleet.ArtifactRef{Registry: "http://harbor.example:8080", Repository: "satellite/" + kind + "-state/" + name + "/state", Tag: "latest"}
	}
	state := fleet.DesiredState{Satellite: 7, GroupStates: []fleet.ArtifactRef{ref("group", "g1"), ref("group", "g2")}, ConfigState: ref("config", "edge-config")}

	doc, digest, err := fleet.RenderSatelliteDesiredState(state, fleet.RobotProjects([]string{"library", "library"}), false)

	require.NoError(t, err)
	require.Equal(t, goldenDesiredSatellite, string(doc))
	require.Equal(t, goldenDesiredSatelliteDigest, digest)
}

func TestRenderSatelliteDesiredState_RobotDisabledOnlyWhenTrue(t *testing.T) {
	pending.Plan(t, "4.3", pendingLifecycle)
	state := fleet.DesiredState{Satellite: 7, ConfigState: stateRef("config", "edge-config")}

	enabled, enabledDigest, err := fleet.RenderSatelliteDesiredState(state, []string{"library", "satellite"}, false)
	require.NoError(t, err)
	disabled, disabledDigest, err := fleet.RenderSatelliteDesiredState(state, []string{"library", "satellite"}, true)
	require.NoError(t, err)

	require.NotContains(t, string(enabled), "robot_disabled")
	require.Contains(t, string(disabled), `"robot_disabled":true`)
	require.NotEqual(t, enabledDigest, disabledDigest)
}
