package user_test

import (
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/clitest"
	"github.com/stretchr/testify/require"
)

func TestUserCommandsCallTheirRoutes(t *testing.T) {
	t.Setenv("GROUND_CONTROL_USER_PASSWORD", "Initial-Passw0rd")
	t.Setenv("GROUND_CONTROL_CURRENT_PASSWORD", "Old-Passw0rd")
	t.Setenv("GROUND_CONTROL_NEW_PASSWORD", "New-Passw0rd")
	clitest.CheckRoutes(t, []clitest.Route{
		{Name: "list", Args: []string{"get", "users"}, Answer: `[]`, Want: clitest.Request{Method: http.MethodGet, Path: "/api/users"}},
		{Name: "get", Args: []string{"get", "user", "--username", "ops"}, Want: clitest.Request{Method: http.MethodGet, Path: "/api/users/ops"}},
		{Name: "delete", Args: []string{"delete", "user", "--username", "ops"}, Status: http.StatusNoContent, Want: clitest.Request{Method: http.MethodDelete, Path: "/api/users/ops"}},
		{
			Name: "create takes the password from the environment", Args: []string{"create", "user", "--username", "ops"}, Status: http.StatusCreated,
			Want: clitest.Request{Method: http.MethodPost, Path: "/api/users", Body: map[string]any{"username": "ops", "password": "Initial-Passw0rd"}},
		},
		{
			Name: "reset a user's password", Args: []string{"update", "user-password", "--username", "ops"}, Status: http.StatusNoContent,
			Want: clitest.Request{Method: http.MethodPatch, Path: "/api/users/ops/password", Body: map[string]any{"new_password": "New-Passw0rd"}},
		},
		{
			Name: "change own password", Args: []string{"update", "own-password"}, Status: http.StatusNoContent,
			Want: clitest.Request{Method: http.MethodPatch, Path: "/api/users/password", Body: map[string]any{
				"current_password": "Old-Passw0rd", "new_password": "New-Passw0rd",
			}},
		},
	})
}

func TestUserCommandsNeedTheirPasswordEnvironment(t *testing.T) {
	server := clitest.NewServer(t, http.StatusOK, `{}`)
	for _, tt := range []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{name: "create", args: []string{"create", "user", "--username", "ops"}, want: "GROUND_CONTROL_USER_PASSWORD"},
		{name: "reset", args: []string{"update", "user-password", "--username", "ops"}, want: "GROUND_CONTROL_NEW_PASSWORD"},
		{name: "own without current", env: map[string]string{"GROUND_CONTROL_NEW_PASSWORD": "New-Passw0rd"}, args: []string{"update", "own-password"}, want: "GROUND_CONTROL_CURRENT_PASSWORD"},
		{name: "own without new", env: map[string]string{"GROUND_CONTROL_CURRENT_PASSWORD": "Old-Passw0rd"}, args: []string{"update", "own-password"}, want: "GROUND_CONTROL_NEW_PASSWORD"},
		{name: "username", env: map[string]string{"GROUND_CONTROL_USER_PASSWORD": "x"}, args: []string{"create", "user", "--username", " "}, want: "--username must not be empty"},
		{name: "get username", args: []string{"get", "user", "--username", ""}, want: "--username must not be empty"},
		{name: "delete username", args: []string{"delete", "user", "--username", ""}, want: "--username must not be empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{"GROUND_CONTROL_USER_PASSWORD", "GROUND_CONTROL_CURRENT_PASSWORD", "GROUND_CONTROL_NEW_PASSWORD"} {
				t.Setenv(key, tt.env[key])
			}
			_, err := clitest.RunAuthenticated(t, server, nil, tt.args...)
			require.ErrorContains(t, err, tt.want)
		})
	}
	require.Empty(t, server.Requests())
}

func TestFailedOwnPasswordChangeShowsTheError(t *testing.T) {
	t.Setenv("GROUND_CONTROL_CURRENT_PASSWORD", "wrong")
	t.Setenv("GROUND_CONTROL_NEW_PASSWORD", "New-Passw0rd")
	server := clitest.NewServer(t, http.StatusUnauthorized, `{"code":401,"message":"current password is incorrect"}`)
	_, err := clitest.RunAuthenticated(t, server, nil, "update", "own-password")
	require.ErrorContains(t, err, "current password is incorrect")
}
