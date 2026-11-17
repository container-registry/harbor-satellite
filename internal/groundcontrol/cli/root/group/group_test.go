package group_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/clitest"
	"github.com/stretchr/testify/require"
)

const manifest = `group: edge
registry: https://harbor.example
artifacts:
  - repository: library/nginx
    tag: ["1.27"]
    type: image
    digest: sha256:0000000000000000000000000000000000000000000000000000000000000000
`

func TestGroupCommandsCallTheirRoutes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "group.yaml")
	require.NoError(t, os.WriteFile(file, []byte(manifest), 0o600))
	syncBody := map[string]any{
		"group": "edge", "registry": "https://harbor.example",
		"artifacts": []any{map[string]any{
			"repository": "library/nginx", "tag": []any{"1.27"}, "type": "image",
			"digest": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		}},
	}
	clitest.CheckRoutes(t, []clitest.Route{
		{Name: "list", Args: []string{"get", "groups"}, Answer: `[{"group_name":"edge"}]`, Want: clitest.Request{Method: http.MethodGet, Path: "/api/groups"}},
		{Name: "get", Args: []string{"get", "group", "--name", "edge"}, Want: clitest.Request{Method: http.MethodGet, Path: "/api/groups/edge"}},
		{
			Name: "get escapes the name", Args: []string{"get", "group", "--name", "a/b"},
			Want: clitest.Request{Method: http.MethodGet, Path: "/api/groups/a/b", RawPath: "/api/groups/a%2Fb"},
		},
		{Name: "satellites", Args: []string{"get", "group-satellites", "--group", "edge"}, Answer: `[]`, Want: clitest.Request{Method: http.MethodGet, Path: "/api/groups/edge/satellites"}},
		{Name: "delete", Args: []string{"delete", "group", "--name", "edge"}, Status: http.StatusNoContent, Want: clitest.Request{Method: http.MethodDelete, Path: "/api/groups/edge"}},
		{
			Name: "add satellite", Args: []string{"add", "satellite-to-group", "--satellite", "edge-01", "--group", "edge"},
			Want: clitest.Request{Method: http.MethodPost, Path: "/api/groups/satellite", Body: map[string]any{"satellite": "edge-01", "group": "edge"}},
		},
		{
			Name: "remove satellite", Args: []string{"remove", "satellite-from-group", "--satellite", "edge-01", "--group", "edge"},
			Want: clitest.Request{Method: http.MethodDelete, Path: "/api/groups/satellite", Body: map[string]any{"satellite": "edge-01", "group": "edge"}},
		},
		{Name: "sync from file", Args: []string{"sync", "group", "-f", file}, Want: clitest.Request{Method: http.MethodPost, Path: "/api/groups/sync", Body: syncBody}},
		{Name: "sync from stdin", Args: []string{"sync", "group", "-f", "-"}, Stdin: manifest, Want: clitest.Request{Method: http.MethodPost, Path: "/api/groups/sync", Body: syncBody}},
	})
}

func TestGroupCommandsValidateBeforeCalling(t *testing.T) {
	server := clitest.NewServer(t, http.StatusOK, `{}`)
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("group: [unclosed"), 0o600))
	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"get", "group", "--name", " "}, want: "--name must not be empty"},
		{args: []string{"get", "group-satellites", "--group", ""}, want: "--group must not be empty"},
		{args: []string{"delete", "group", "--name", ""}, want: "--name must not be empty"},
		{args: []string{"add", "satellite-to-group", "--satellite", "edge-01", "--group", ""}, want: "--group must not be empty"},
		{args: []string{"remove", "satellite-from-group", "--satellite", "", "--group", "edge"}, want: "--satellite must not be empty"},
		{args: []string{"sync", "group", "-f", filepath.Join(t.TempDir(), "missing.yaml")}, want: "no such file or directory"},
		{args: []string{"sync", "group", "-f", bad}, want: "error converting YAML to JSON"},
	} {
		_, err := clitest.RunAuthenticated(t, server, nil, tt.args...)
		require.ErrorContains(t, err, tt.want, "%v", tt.args)
	}
	require.Empty(t, server.Requests())
}

func TestGroupCommandShowsServerError(t *testing.T) {
	server := clitest.NewServer(t, http.StatusNotFound, `{"code":404,"message":"group not found"}`)
	_, err := clitest.RunAuthenticated(t, server, nil, "get", "group", "--name", "edge")
	require.ErrorContains(t, err, "group not found")
}

func TestGroupCommandNeedsAToken(t *testing.T) {
	server := clitest.NewServer(t, http.StatusOK, `[]`)
	t.Setenv("GROUND_CONTROL_TOKEN", "")
	_, err := clitest.Run(t, nil, "--server", server.URL, "get", "groups")
	require.Error(t, err)
	require.Empty(t, server.Requests())
}
