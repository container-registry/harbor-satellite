package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashRobotCredentials(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{
			name:   "basic secret",
			secret: "s3cret-value-123",
		},
		{
			name:   "empty secret",
			secret: "",
		},
		{
			name:   "long secret",
			secret: strings.Repeat("a", 256),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := hashRobotCredentials(tt.secret)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(hash, "$argon2id$"), "hash should start with $argon2id$")
		})
	}

	t.Run("random salt produces unique hashes", func(t *testing.T) {
		h1, err := hashRobotCredentials("same-secret")
		require.NoError(t, err)
		h2, err := hashRobotCredentials("same-secret")
		require.NoError(t, err)

		require.NotEqual(t, h1, h2, "same secret with random salt should produce different hashes")
	})

	t.Run("different secrets produce different hashes", func(t *testing.T) {
		h1, err := hashRobotCredentials("secret-1")
		require.NoError(t, err)
		h2, err := hashRobotCredentials("secret-2")
		require.NoError(t, err)

		require.NotEqual(t, h1, h2)
	})
}

func TestVerifyRobotCredentials(t *testing.T) {
	secret := "correct-secret"
	storedHash, err := hashRobotCredentials(secret)
	require.NoError(t, err)

	tests := []struct {
		name   string
		secret string
		want   bool
	}{
		{
			name:   "correct secret",
			secret: secret,
			want:   true,
		},
		{
			name:   "wrong secret",
			secret: "wrong-secret",
			want:   false,
		},
		{
			name:   "empty secret",
			secret: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifyRobotCredentials(tt.secret, storedHash)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("malformed hash returns false", func(t *testing.T) {
		require.False(t, verifyRobotCredentials(secret, "not-a-valid-hash"))
	})
}
