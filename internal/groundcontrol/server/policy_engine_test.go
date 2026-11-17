package server

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

// casbin without a policy would silently keep the builtin route check.
func TestNewServer_CasbinWithoutPolicyFails(t *testing.T) {
	t.Setenv("POLICY_ENGINE", env.PolicyEngineCasbin)
	loadGCForTest(t)

	res, err := NewServer(context.Background())

	require.ErrorContains(t, err, "needs a policy")
	if res != nil {
		require.NoError(t, res.Close())
	}
}
