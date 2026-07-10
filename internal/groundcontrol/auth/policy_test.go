package auth

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/stretchr/testify/require"
)

func TestLoadPolicyFromConfigNormalizesInvalidLengths(t *testing.T) {
	policy := LoadPolicyFromConfig(env.PasswordPolicy{
		MinLength:        0,
		MaxLength:        -1,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
	})

	require.Equal(t, 8, policy.MinLength)
	require.Equal(t, 128, policy.MaxLength)
}

func TestLoadPolicyFromConfigEnsuresMinDoesNotExceedMax(t *testing.T) {
	policy := LoadPolicyFromConfig(env.PasswordPolicy{
		MinLength:        16,
		MaxLength:        8,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
	})

	require.Equal(t, 16, policy.MinLength)
	require.Equal(t, 16, policy.MaxLength)
	require.NoError(t, policy.Validate("ValidPassword123"))
}
