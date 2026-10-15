package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/goharbor/go-client/pkg/sdk/v2.0/models"
	"github.com/stretchr/testify/require"
)

// robotUpdatesSince returns, per robot update GC sent to Harbor for robot
// since mark, the namespaces of its payload sorted but not deduplicated.
func robotUpdatesSince(t *testing.T, stub *harbortest.Stub, mark int, robot string) [][]string {
	t.Helper()
	var updates [][]string
	for _, call := range stub.CallsSince(mark) {
		if call.Route != harbortest.RouteUpdateRobot {
			continue
		}
		var payload models.Robot
		require.NoError(t, json.Unmarshal(call.Body, &payload))
		if payload.Name != robot {
			continue
		}
		namespaces := make([]string, 0, len(payload.Permissions))
		for _, p := range payload.Permissions {
			namespaces = append(namespaces, p.Namespace)
		}
		sort.Strings(namespaces)
		updates = append(updates, namespaces)
	}
	return updates
}

// requireRobotNamespaces checks the robot's permissions in Harbor against
// want, duplicates included, whichever calls put them there.
func requireRobotNamespaces(t *testing.T, h *gcHarness, robot string, want ...string) {
	t.Helper()
	r, ok := h.harbor.Robot(robot)
	require.True(t, ok, "robot %s not in Harbor", robot)
	namespaces := make([]string, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		namespaces = append(namespaces, p.Namespace)
	}
	sort.Strings(namespaces)
	require.Equal(t, want, namespaces)
}

func TestSyncRobotPermissions_OneHarborUpdatePerRobot(t *testing.T) {
	pending.Plan(t, "D5", "robot permissions are written per group instead of once for the union of the satellite's groups")
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("group-a", "alpha/app", "shared/lib")
	h.syncGroup("group-b", "beta/app", "shared/lib")
	h.syncGroup("group-c", "gamma/app")
	first := h.onboard("edge-01", "edge-config", "group-a", "group-b", "group-c")
	second := h.onboard("edge-02", "edge-config", "group-b")
	ctx := context.Background()
	var ids []int32
	for _, name := range []string{"edge-01", "edge-02"} {
		sat, err := h.app.dbQueries.GetSatelliteByName(ctx, name)
		require.NoError(t, err)
		ids = append(ids, sat.ID)
	}
	mark := h.harbor.Mark()

	require.NoError(t, syncRobotPermissions(ctx, h.app.dbQueries, ids))

	require.Equal(t, [][]string{{"alpha", "beta", "gamma", "satellite", "shared"}}, robotUpdatesSince(t, h.harbor, mark, first.Username))
	require.Equal(t, [][]string{{"beta", "satellite", "shared"}}, robotUpdatesSince(t, h.harbor, mark, second.Username))
	requireRobotNamespaces(t, h, first.Username, "alpha", "beta", "gamma", "satellite", "shared")
	requireRobotNamespaces(t, h, second.Username, "beta", "satellite", "shared")
}

func TestRobotPermissionsAfter(t *testing.T) {
	pending.Plan(t, "D5", "register and group sync overwrite the robot permissions with one group's projects; add, remove and group delete merge them without removing duplicates")
	setup := func(t *testing.T, groups ...string) (*gcHarness, RegistryCredentials) {
		t.Helper()
		h := newGCHarness(t)
		h.createConfig("edge-config")
		h.syncGroup("group-a", "alpha/app", "shared/lib")
		h.syncGroup("group-b", "beta/app", "shared/lib")
		h.syncGroup("group-c", "gamma/app", "shared/lib")
		return h, h.onboard("edge-01", "edge-config", groups...)
	}

	t.Run("Register", func(t *testing.T) {
		h := newGCHarness(t)
		h.createConfig("edge-config")
		h.syncGroup("group-a", "alpha/app", "shared/lib")
		h.syncGroup("group-b", "beta/app", "shared/lib")

		creds := h.onboard("edge-01", "edge-config", "group-a", "group-b")

		requireRobotNamespaces(t, h, creds.Username, "alpha", "beta", "satellite", "shared")
	})

	t.Run("SyncGroup", func(t *testing.T) {
		h, creds := setup(t, "group-a", "group-b")

		h.syncGroup("group-a", "alpha/app", "delta/lib")

		requireRobotNamespaces(t, h, creds.Username, "alpha", "beta", "delta", "satellite", "shared")
	})

	t.Run("AddToGroup", func(t *testing.T) {
		h, creds := setup(t, "group-a")

		h.mustDo(http.MethodPost, "/api/groups/satellite", SatelliteGroupRequest{Satellite: "edge-01", Group: "group-b"}, h.admin, http.StatusOK)

		requireRobotNamespaces(t, h, creds.Username, "alpha", "beta", "satellite", "shared")
	})

	t.Run("RemoveFromGroup", func(t *testing.T) {
		h, creds := setup(t, "group-a", "group-b", "group-c")

		h.mustDo(http.MethodDelete, "/api/groups/satellite", SatelliteGroupRequest{Satellite: "edge-01", Group: "group-c"}, h.admin, http.StatusOK)

		requireRobotNamespaces(t, h, creds.Username, "alpha", "beta", "satellite", "shared")
	})

	t.Run("DeleteGroup", func(t *testing.T) {
		h, creds := setup(t, "group-a", "group-b", "group-c")

		h.mustDo(http.MethodDelete, "/api/groups/group-c", nil, h.admin, http.StatusOK)

		requireRobotNamespaces(t, h, creds.Username, "alpha", "beta", "satellite", "shared")
	})
}
