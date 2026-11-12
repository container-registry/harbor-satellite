package satellite_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/clitest"
	"github.com/stretchr/testify/require"
)

const spiffeManifest = `satellite_name: edge-01
region: eu
attestation_method: join_token
selectors: ["unix:uid:0"]
ttl_seconds: 600
`

func TestSatelliteCommandsCallTheirRoutes(t *testing.T) {
	spiffeFile := filepath.Join(t.TempDir(), "spiffe.yaml")
	require.NoError(t, os.WriteFile(spiffeFile, []byte(spiffeManifest), 0o600))
	spiffeBody := map[string]any{
		"satellite_name": "edge-01", "region": "eu", "attestation_method": "join_token",
		"selectors": []any{"unix:uid:0"}, "ttl_seconds": float64(600),
	}
	get := func(path string) clitest.Request { return clitest.Request{Method: http.MethodGet, Path: path} }
	clitest.CheckRoutes(t, []clitest.Route{
		{Name: "list", Args: []string{"get", "satellites"}, Answer: `[]`, Want: get("/api/satellites")},
		{Name: "active", Args: []string{"get", "active-satellites"}, Answer: `[]`, Want: get("/api/satellites/active")},
		{Name: "stale", Args: []string{"get", "stale-satellites"}, Answer: `[]`, Want: get("/api/satellites/stale")},
		{Name: "get", Args: []string{"get", "satellite", "--name", "edge-01"}, Want: get("/api/satellites/edge-01")},
		{Name: "cached images", Args: []string{"get", "cached-images", "--satellite", "edge-01"}, Answer: `[]`, Want: get("/api/satellites/edge-01/images")},
		{Name: "status", Args: []string{"get", "satellite-status", "--satellite", "edge-01"}, Want: get("/api/satellites/edge-01/status")},
		{Name: "lifecycle", Args: []string{"get", "satellite-lifecycle", "--satellite", "edge-01"}, Want: get("/api/satellites/edge-01/lifecycle")},
		{Name: "delete", Args: []string{"delete", "satellite", "--name", "edge-01"}, Status: http.StatusNoContent, Want: clitest.Request{Method: http.MethodDelete, Path: "/api/satellites/edge-01"}},
		{Name: "revoke", Args: []string{"revoke", "satellite", "--name", "edge-01"}, Want: clitest.Request{Method: http.MethodPost, Path: "/api/satellites/edge-01/revoke"}},
		{Name: "unrevoke", Args: []string{"unrevoke", "satellite", "--name", "edge-01"}, Want: clitest.Request{Method: http.MethodPost, Path: "/api/satellites/edge-01/unrevoke"}},
		{
			Name: "register with groups", Args: []string{"register", "satellite", "--name", "edge-01", "--config", "default", "--group", "a,b", "--group", "c"},
			Status: http.StatusCreated,
			Want: clitest.Request{Method: http.MethodPost, Path: "/api/satellites", Body: map[string]any{
				"name": "edge-01", "config_name": "default", "groups": []any{"a", "b", "c"},
			}},
		},
		{
			Name: "register without groups", Args: []string{"register", "satellite", "--name", "edge-01", "--config", "default"},
			Status: http.StatusCreated,
			Want:   clitest.Request{Method: http.MethodPost, Path: "/api/satellites", Body: map[string]any{"name": "edge-01", "config_name": "default"}},
		},
		{
			Name: "register SPIFFE", Args: []string{"register", "spiffe-satellite", "-f", spiffeFile}, Status: http.StatusCreated,
			Want: clitest.Request{Method: http.MethodPost, Path: "/api/satellites/register", Body: spiffeBody},
		},
		{
			Name: "assign config", Args: []string{"update", "satellite-config", "--satellite", "edge-01", "--config", "edge"},
			Want: clitest.Request{Method: http.MethodPost, Path: "/api/configs/satellite", Body: map[string]any{"satellite": "edge-01", "config_name": "edge"}},
		},
	})
}

func TestSatelliteCommandsValidateBeforeCalling(t *testing.T) {
	server := clitest.NewServer(t, http.StatusOK, `{}`)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"get", "satellite", "--name", ""}, want: "--name must not be empty"},
		{args: []string{"get", "cached-images", "--satellite", " "}, want: "--satellite must not be empty"},
		{args: []string{"get", "satellite-status", "--satellite", ""}, want: "--satellite must not be empty"},
		{args: []string{"get", "satellite-lifecycle", "--satellite", ""}, want: "--satellite must not be empty"},
		{args: []string{"delete", "satellite", "--name", ""}, want: "--name must not be empty"},
		{args: []string{"unrevoke", "satellite", "--name", ""}, want: "--name must not be empty"},
		{args: []string{"register", "satellite", "--name", "edge-01", "--config", ""}, want: "--config must not be empty"},
		{args: []string{"register", "spiffe-satellite", "-f", filepath.Join(t.TempDir(), "none.yaml")}, want: "open request file"},
		{args: []string{"register", "spiffe-satellite", "-f", "-", "--name", "x"}, want: "unknown flag: --name"},
		{args: []string{"update", "satellite-config", "--satellite", "", "--config", "edge"}, want: "--satellite must not be empty"},
		{args: []string{"get", "satellite"}, want: "--name must not be empty"},
	} {
		_, err := clitest.RunAuthenticated(t, server, nil, tt.args...)
		require.ErrorContains(t, err, tt.want, "%v", tt.args)
	}
	require.Empty(t, server.Requests())
}

func TestSatelliteCommandShowsServerError(t *testing.T) {
	server := clitest.NewServer(t, http.StatusForbidden, `{"code":403,"message":"satellite revoked"}`)
	_, err := clitest.RunAuthenticated(t, server, nil, "get", "satellite-status", "--satellite", "edge-01")
	require.ErrorContains(t, err, "403 Forbidden")
	require.ErrorContains(t, err, "satellite revoked")
}
