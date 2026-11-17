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
	"github.com/stretchr/testify/require"
)

// requireRobotNamespaces checks the robot's permissions in Harbor against
// want, duplicates included, whichever calls put them there.
func requireRobotNamespaces(t *testing.T, h *gcHarness, robot string, want ...string) {
	t.Helper()
	h.drainOutbox()
	r, ok := h.harbor.Robot(robot)
	require.True(t, ok, "robot %s not in Harbor", robot)
	namespaces := make([]string, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		namespaces = append(namespaces, p.Namespace)
	}
	sort.Strings(namespaces)
	require.Equal(t, want, namespaces)
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
		grp, groupErr := h.app.dbQueries.GetGroupByName(ctx, name)
		require.NoError(t, groupErr)
		require.NoError(t, h.app.dbQueries.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{SatelliteID: sat.ID, GroupID: grp.ID}))
	}

	req := httptest.NewRequest(http.MethodGet, "/satellites/spiffe-ztr", nil)
	id, err := url.Parse("spiffe://harbor-satellite.local/satellite/region/default/spiffe-01")
	require.NoError(t, err)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{id}}}}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "the async plane creates the robot first: %s", rec.Body.String())
	require.NotEmpty(t, rec.Header().Get("Retry-After"))

	h.drainOutbox()
	rec = httptest.NewRecorder()
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
