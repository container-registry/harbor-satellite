package auth

import (
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
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

// Regression test for #666: password length must be measured in characters
// (runes), not bytes. Non-ASCII characters are multi-byte, so len() and rune
// count differ for the same password.
func TestPasswordPolicy_Validate_LengthCountsRunes(t *testing.T) {
	policy := DefaultPolicy()

	t.Run("rejects fewer than MinLength characters even when bytes suffice", func(t *testing.T) {
		require.Error(t, policy.Validate("Σαβγ1")) // 5 runes, 9 bytes
	})

	t.Run("accepts MinLength non-ASCII characters", func(t *testing.T) {
		require.NoError(t, policy.Validate("Σαβγδεζη1")) // 9 runes, 15 bytes
	})

	t.Run("does not falsely reject non-ASCII passwords within MaxLength", func(t *testing.T) {
		require.NoError(t, policy.Validate("A1"+strings.Repeat("é", 98))) // 100 runes, 198 bytes
	})

	t.Run("rejects passwords exceeding MaxLength characters", func(t *testing.T) {
		require.Error(t, policy.Validate("A1"+strings.Repeat("a", 127))) // 129 runes, 129 bytes; only violation is MaxLength
	})
}
