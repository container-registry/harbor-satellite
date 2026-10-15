package state

import (
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestResetReplicatedEntities_ForcesFullReplication(t *testing.T) {
	pending.Plan(t, "5.1", "nothing resyncs the state after the OCI index is recovered")
	path := filepath.Join(t.TempDir(), "state.json")
	groups := []StateMap{
		{url: "http://registry.example.com/group1", Entities: []Entity{{Name: "alpine", Repository: "library", Tag: "latest", Digest: "sha256:abc"}}},
		{url: "http://registry.example.com/group2", Entities: []Entity{{Name: "nginx", Repository: "library", Tag: "1.27", Digest: "sha256:def"}}},
	}
	require.NoError(t, SaveState(path, groups, "sha256:config"))
	nop := zerolog.Nop()
	process := NewFetchAndReplicateStateProcess(nil, path, t.TempDir(), &nop)
	require.Len(t, process.stateMap, 2)

	require.NoError(t, process.ResetReplicatedEntities())

	for _, group := range process.stateMap {
		require.Empty(t, group.Entities, group.url)
	}
	persisted, err := LoadState(path)
	require.NoError(t, err)
	require.Len(t, persisted.Groups, 2)
	for i, group := range persisted.Groups {
		require.Equal(t, groups[i].url, group.URL)
		require.Empty(t, group.Entities, "the next Execute must see every entity as new")
	}
}
