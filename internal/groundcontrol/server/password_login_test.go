package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// With OIDC_DISABLE_PASSWORD_LOGIN only the bootstrap admin, the
// break-glass account, logs in with a password (plan item 8.2).
func TestPasswordLogin_DisabledExceptBootstrap(t *testing.T) {
	t.Setenv("OIDC_DISABLE_PASSWORD_LOGIN", "true")
	h := newGCHarness(t)
	h.mustDo(http.MethodPost, "/api/users", CreateUserRequest{Username: "operator", Password: "Operat0rPass"}, h.admin, http.StatusCreated)

	r := h.do(http.MethodPost, "/login", LoginRequest{Username: "operator", Password: "Operat0rPass"}, nil)
	require.Equal(t, http.StatusUnauthorized, r.Status, string(r.Body))
	require.Contains(t, string(r.Body), errPasswordLoginDisabled.Error())
	h.mustDo(http.MethodGet, "/api/satellites", nil, basicAuth("operator", "Operat0rPass"), http.StatusUnauthorized)

	h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil, http.StatusOK)
}
