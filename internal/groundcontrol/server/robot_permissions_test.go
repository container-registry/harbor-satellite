package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbortest"
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

func TestSPIFFEZTR_RobotGetsProjectsOfExistingGroups(t *testing.T) {
	if !spiffeBuild {
		t.Skip("SPIFFE is compiled out")
	}
	h := newGCHarness(t)
	h.createConfig("default")
	h.syncGroup("group-a", "alpha/app")
	h.syncGroup("group-b", "beta/app")
	ctx := context.Background()
	// A satellite in two groups without a robot: what a satellite added to
	// groups before its first SPIFFE ZTR looks like.
	sat, err := h.app.dbQueries.CreateSatellite(ctx, "spiffe-01")
	require.NoError(t, err)
	for _, name := range []string{"group-a", "group-b"} {
		grp, err := h.app.dbQueries.GetGroupByName(ctx, name)
		require.NoError(t, err)
		require.NoError(t, h.app.dbQueries.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: sat.ID, GroupID: grp.ID}))
	}

	req := httptest.NewRequest(http.MethodGet, "/satellites/spiffe-ztr", nil)
	id, err := url.Parse("spiffe://harbor-satellite.local/satellite/region/default/spiffe-01")
	require.NoError(t, err)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{id}}}}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var cfg StateConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cfg))
	requireRobotNamespaces(t, h, cfg.Auth.Username, "alpha", "beta", "satellite")
}

func TestRobotPermissionsAfterSyncGroupDropRemovedProject(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("group-a", "alpha/app", "old/app")
	h.syncGroup("group-b", "beta/app")
	creds := h.onboard("edge-01", "edge-config", "group-a", "group-b")

	h.syncGroup("group-a", "alpha/app")

	requireRobotNamespaces(t, h, creds.Username, "alpha", "beta", "satellite")
}
