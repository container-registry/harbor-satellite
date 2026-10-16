package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOIDC_ValidateRoleMap(t *testing.T) {
	require.NoError(t, OIDC{}.Validate())
	require.NoError(t, OIDC{RoleMap: map[string]string{"edge-ops": "admin", "platform": "system_admin"}}.Validate())
	require.ErrorContains(t, OIDC{RoleMap: map[string]string{"edge-ops": "root"}}.Validate(), "OIDC_ROLE_MAP edge-ops=root")
}
