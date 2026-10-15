package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const goldenRegistry = "http://harbor.example:8080"

// goldenGroupState is a group state as createStateArtifact pushes it, and
// goldenGroupStateDigest the digest of its encoding today. A change of
// either type that alters the bytes changes the digest and makes every
// satellite refetch every group.
var goldenGroupState = GroupSyncRequest{
	Group:    "edge-group",
	Registry: goldenRegistry,
	Artifacts: []GroupArtifact{
		{Repository: "library/alpine", Tag: []string{"3.20", "latest"}, Type: "image", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000001"},
		{Repository: "team/app", Tag: []string{"v1"}, Type: "image", Digest: "sha256:0000000000000000000000000000000000000000000000000000000000000002", Labels: map[string]any{"tier": "edge"}},
		{Repository: "gone/old", Tag: []string{"v0"}, Type: "image", Deleted: true},
	},
}

const goldenGroupStateDigest = fleet.Digest("sha256:144aeeeae7b768989948b488c67a3ea7414f51b6f6805bfa52f82a6b9a6043f8")

func sha256Digest(b []byte) fleet.Digest {
	sum := sha256.Sum256(b)
	return fleet.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func toFleetArtifact(a GroupArtifact) fleet.Artifact {
	return fleet.Artifact{Deleted: a.Deleted, Digest: a.Digest, Labels: a.Labels, Repository: a.Repository, Tags: a.Tag, Type: a.Type}
}

func TestGroupStateGoldenDigest(t *testing.T) {
	payload, err := json.Marshal(goldenGroupState)
	require.NoError(t, err)

	require.Equal(t, goldenGroupStateDigest, sha256Digest(payload), "%s", payload)
}

// goldenSatelliteStates are satellite states as createOrUpdateSatStateArtifact
// encodes them, with and without groups. Today a satellite without groups
// gets no artifact at all (D10); the renderer must give it this one, and
// the same bytes every time so the reconciler does not push it again.
var goldenSatelliteStates = map[string]struct {
	state  SatelliteStateArtifact
	digest fleet.Digest
}{
	"with groups": {SatelliteStateArtifact{
		Config: goldenRegistry + "/satellite/config-state/edge-config/state:latest",
		States: []string{goldenRegistry + "/satellite/group-state/g1/state:latest", goldenRegistry + "/satellite/group-state/g2/state:latest"},
	}, "sha256:aa7c5e68ff292605f040696fbc7ad571e743fd8386ae7b9833c3e09841167cb8"},
	"without groups": {SatelliteStateArtifact{
		Config: goldenRegistry + "/satellite/config-state/edge-config/state:latest",
	}, "sha256:91d54dd6be877431a1a415912ee0c978d1d3c0642b62de2c27e0effc690c3405"},
}

func TestSatelliteStateGoldenDigests(t *testing.T) {
	for name, golden := range goldenSatelliteStates {
		payload, err := json.Marshal(golden.state)
		require.NoError(t, err)
		require.Equal(t, golden.digest, sha256Digest(payload), "%s: %s", name, payload)
	}
}

func TestFleetArtifactEncodesLikeGroupArtifact(t *testing.T) {
	for _, in := range []string{
		`{}`,
		`{"repository":"library/alpine","tag":["3.20"],"type":"image","digest":"sha256:01"}`,
		`{"repository":"gone/old","deleted":true}`,
		`{"repository":"a/b","labels":null}`,
		`{"repository":"a/b","labels":{}}`,
		`{"repository":"a/b","labels":{"tier":"edge","n":1}}`,
		`{"repository":"a/b","labels":["x"],"tag":[]}`,
	} {
		var api GroupArtifact
		require.NoError(t, json.Unmarshal([]byte(in), &api))
		var domain fleet.Artifact
		require.NoError(t, json.Unmarshal([]byte(in), &domain))

		want, err := json.Marshal(api)
		require.NoError(t, err)
		got, err := json.Marshal(domain)
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), "input %s", in)
		converted, err := json.Marshal(toFleetArtifact(api))
		require.NoError(t, err)
		require.Equal(t, string(want), string(converted), "input %s", in)
	}
}

func TestRenderGroupState_MatchesGoldenDigest(t *testing.T) {
	pending.Plan(t, "1.2", "group state is rendered by createStateArtifact from the request")
	group := fleet.Group{Name: goldenGroupState.Group}
	for _, a := range goldenGroupState.Artifacts {
		group.Artifacts = append(group.Artifacts, toFleetArtifact(a))
	}
	want, err := json.Marshal(goldenGroupState)
	require.NoError(t, err)

	payload, digest, err := fleet.RenderGroupState(group, goldenRegistry)

	require.NoError(t, err)
	require.Equal(t, string(want), string(payload))
	require.Equal(t, goldenGroupStateDigest, digest)
}

func TestRenderSatelliteState_MatchesServerType(t *testing.T) {
	pending.Plan(t, "1.2", "satellite state is rendered by createOrUpdateSatStateArtifact")
	ref := func(kind, name string) fleet.ArtifactRef {
		return fleet.ArtifactRef{Registry: goldenRegistry, Repository: "satellite/" + kind + "-state/" + name + "/state", Tag: "latest"}
	}
	for name, state := range map[string]fleet.DesiredState{
		"with groups":    {Satellite: 7, GroupStates: []fleet.ArtifactRef{ref("group", "g1"), ref("group", "g2")}, ConfigState: ref("config", "edge-config")},
		"without groups": {Satellite: 7, ConfigState: ref("config", "edge-config")},
	} {
		t.Run(name, func(t *testing.T) {
			api := SatelliteStateArtifact{Config: state.ConfigState.String()}
			for _, g := range state.GroupStates {
				api.States = append(api.States, g.String())
			}
			want, err := json.Marshal(api)
			require.NoError(t, err)

			payload, digest, err := fleet.RenderSatelliteState(state)

			require.NoError(t, err)
			require.Equal(t, string(want), string(payload))
			require.Equal(t, goldenSatelliteStates[name].digest, digest)
		})
	}
}
