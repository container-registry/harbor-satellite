package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/stretchr/testify/require"
)

func TestCreateOrUpdateSatStateArtifact(t *testing.T) {
	previous := env.GC.Harbor
	t.Cleanup(func() { env.GC.Harbor = previous })
	env.GC.Harbor = env.Harbor{}

	t.Run("rejects an empty satellite name", func(t *testing.T) {
		err := createOrUpdateSatStateArtifact(context.Background(), "", []string{"state"}, "cfg")
		require.ErrorContains(t, err, "satellite name")
	})

	// Before the fix, an empty group list returned nil before ever reaching
	// pushStateArtifact, so a satellite with no groups left never got a
	// cleared state published. Reaching the Harbor validation error here
	// (raised inside pushStateArtifact) is what proves that early return is
	// gone for both a nil and an explicitly empty states slice.
	t.Run("reaches harbor validation instead of returning early for empty states", func(t *testing.T) {
		tests := []struct {
			name   string
			states []string
		}{
			{name: "nil states", states: nil},
			{name: "empty states", states: []string{}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := createOrUpdateSatStateArtifact(context.Background(), "sat-1", tt.states, "cfg")
				require.ErrorContains(t, err, "HARBOR_URL")
			})
		}
	})
}

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
			hash, err := crypto.HashSecret(tt.secret)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(hash, "$argon2id$"), "hash should start with $argon2id$")
		})
	}

	t.Run("random salt produces unique hashes", func(t *testing.T) {
		h1, err := crypto.HashSecret("same-secret")
		require.NoError(t, err)
		h2, err := crypto.HashSecret("same-secret")
		require.NoError(t, err)

		require.NotEqual(t, h1, h2, "same secret with random salt should produce different hashes")
	})

	t.Run("different secrets produce different hashes", func(t *testing.T) {
		h1, err := crypto.HashSecret("secret-1")
		require.NoError(t, err)
		h2, err := crypto.HashSecret("secret-2")
		require.NoError(t, err)

		require.NotEqual(t, h1, h2)
	})
}

func TestVerifyRobotCredentials(t *testing.T) {
	secret := "correct-secret"
	storedHash, err := crypto.HashSecret(secret)
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
			got := crypto.VerifySecret(tt.secret, storedHash)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("malformed hash returns false", func(t *testing.T) {
		require.False(t, crypto.VerifySecret(secret, "not-a-valid-hash"))
	})
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	require.NoError(t, err)
	return body
}
