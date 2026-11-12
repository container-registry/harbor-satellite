package config_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/clitest"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestConfigCommandsCallTheirRoutes(t *testing.T) {
	create := writeFile(t, "config.yaml", "config_name: edge\nconfig:\n  app_config:\n    log_level: debug\n")
	patch := writeFile(t, "patch.json", `{"app_config":{"log_level":"warn","audit":null}}`)
	clitest.CheckRoutes(t, []clitest.Route{
		{Name: "list", Args: []string{"get", "configs"}, Answer: `[{"config_name":"edge"}]`, Want: clitest.Request{Method: http.MethodGet, Path: "/api/configs"}},
		{Name: "get", Args: []string{"get", "config", "--name", "edge"}, Want: clitest.Request{Method: http.MethodGet, Path: "/api/configs/edge"}},
		{Name: "delete", Args: []string{"delete", "config", "--name", "edge"}, Status: http.StatusNoContent, Want: clitest.Request{Method: http.MethodDelete, Path: "/api/configs/edge"}},
		{
			Name: "create", Args: []string{"create", "config", "-f", create}, Status: http.StatusCreated,
			Want: clitest.Request{Method: http.MethodPost, Path: "/api/configs", Body: map[string]any{
				"config_name": "edge", "config": map[string]any{"app_config": map[string]any{"log_level": "debug"}},
			}},
		},
		{
			Name: "update sends the patch as is, nulls included", Args: []string{"update", "config", "--name", "edge", "-f", patch},
			Want: clitest.Request{Method: http.MethodPatch, Path: "/api/configs/edge", Body: map[string]any{
				"app_config": map[string]any{"log_level": "warn", "audit": nil},
			}},
		},
		{
			Name: "update from YAML on stdin", Args: []string{"update", "config", "--name", "edge", "-f", "-"}, Stdin: "app_config:\n  log_level: error\n",
			Want: clitest.Request{Method: http.MethodPatch, Path: "/api/configs/edge", Body: map[string]any{
				"app_config": map[string]any{"log_level": "error"},
			}},
		},
	})
}

func TestConfigCommandsValidateBeforeCalling(t *testing.T) {
	server := clitest.NewServer(t, http.StatusOK, `{}`)
	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"get", "config", "--name", ""}, want: "--name must not be empty"},
		{args: []string{"delete", "config", "--name", " "}, want: "--name must not be empty"},
		{args: []string{"create", "config", "-f", ""}, want: "--file must not be empty"},
		{args: []string{"create", "config", "-f", writeFile(t, "unknown.yaml", "config_name: edge\nunknown_field: 1\n")}, want: `unknown field "unknown_field"`},
		{args: []string{"update", "config", "--name", "", "-f", writeFile(t, "p.json", `{}`)}, want: "--name must not be empty"},
		{args: []string{"update", "config", "--name", "edge", "-f", writeFile(t, "list.yaml", "- not an object\n")}, want: "cannot unmarshal array"},
		{args: []string{"update", "config", "--name", "edge", "-f", writeFile(t, "empty.yaml", "")}, want: "manifest must be an object"},
	} {
		_, err := clitest.RunAuthenticated(t, server, nil, tt.args...)
		require.ErrorContains(t, err, tt.want, "%v", tt.args)
	}
	require.Empty(t, server.Requests())
}

func TestConfigCommandShowsServerError(t *testing.T) {
	server := clitest.NewServer(t, http.StatusConflict, `{"code":409,"message":"config is in use"}`)
	_, err := clitest.RunAuthenticated(t, server, nil, "delete", "config", "--name", "edge")
	require.ErrorContains(t, err, "config is in use")
}
