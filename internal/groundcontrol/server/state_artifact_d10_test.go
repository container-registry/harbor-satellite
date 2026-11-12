package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/stretchr/testify/require"
)

// requireEmptyStatePublished checks that GC pushed the state artifact of
// satellite edge-01 since mark and that it lists no group and the given
// config. The
// generated SatelliteStateArtifact omits an empty states list, which the
// satellite reads the same as "states": [].
func requireEmptyStatePublished(t *testing.T, h *gcHarness, mark int, config string) {
	t.Helper()
	const repository = "satellite/satellite-state/edge-01/state"
	pushed := false
	for _, call := range h.harbor.CallsSince(mark) {
		if call.Route == harbortest.RoutePutManifest && strings.HasPrefix(call.Path, "/v2/"+repository+"/manifests/") {
			pushed = true
		}
	}
	require.True(t, pushed, "GC did not push %s", repository)

	data, err := h.harbor.ArtifactFile(repository, "latest", "artifacts.json")
	require.NoError(t, err)
	var state SatelliteStateArtifact
	require.NoError(t, json.Unmarshal(data, &state), string(data))
	require.Empty(t, state.States, string(data))
	require.True(t, strings.HasSuffix(state.Config, "/satellite/config-state/"+config+"/state:latest"), string(data))
}

func TestRemoveFromLastGroup_PublishesEmptyState(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("group-a", "library/alpine")
	h.onboard("edge-01", "edge-config", "group-a")
	mark := h.harbor.Mark()

	h.mustDo(http.MethodDelete, "/api/groups/satellite", SatelliteGroupRequest{Satellite: "edge-01", Group: "group-a"}, h.admin, http.StatusOK)

	requireEmptyStatePublished(t, h, mark, "edge-config")
}

func TestDeleteLastGroup_PublishesEmptyState(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("group-a", "library/alpine")
	h.onboard("edge-01", "edge-config", "group-a")
	mark := h.harbor.Mark()

	h.mustDo(http.MethodDelete, "/api/groups/group-a", nil, h.admin, http.StatusOK)

	requireEmptyStatePublished(t, h, mark, "edge-config")
}

func TestRegisterWithoutGroups_PublishesEmptyState(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	mark := h.harbor.Mark()

	h.registerSatellite("edge-01", "edge-config")

	requireEmptyStatePublished(t, h, mark, "edge-config")
}

func TestSetConfigWithoutGroups_PublishesState(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.createConfig("edge-config-2")
	h.registerSatellite("edge-01", "edge-config")
	mark := h.harbor.Mark()

	h.mustDo(http.MethodPost, "/api/configs/satellite", SatelliteConfigRequest{Satellite: "edge-01", ConfigName: "edge-config-2"}, h.admin, http.StatusOK)

	requireEmptyStatePublished(t, h, mark, "edge-config-2")
}
