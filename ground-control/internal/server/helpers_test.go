package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashRobotCredentials(t *testing.T) {
	tests := []struct {
		name      string
		robotName string
		secret    string
	}{
		{
			name:      "basic credentials",
			robotName: "robot$satellite+edge-01",
			secret:    "s3cret-value-123",
		},
		{
			name:      "empty secret",
			robotName: "robot$satellite+edge-02",
			secret:    "",
		},
		{
			name:      "long secret",
			robotName: "robot$satellite+edge-03",
			secret:    strings.Repeat("a", 256),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hashRobotCredentials(tt.robotName, tt.secret)

			require.True(t, strings.HasPrefix(hash, "$argon2id$"), "hash should start with $argon2id$")

			// deterministic: same input produces same hash
			hash2 := hashRobotCredentials(tt.robotName, tt.secret)
			require.Equal(t, hash, hash2, "same input should produce identical hash")
		})
	}

	// different inputs produce different hashes
	t.Run("different inputs produce different hashes", func(t *testing.T) {
		h1 := hashRobotCredentials("robot-a", "secret-1")
		h2 := hashRobotCredentials("robot-a", "secret-2")
		h3 := hashRobotCredentials("robot-b", "secret-1")

		require.NotEqual(t, h1, h2, "different secrets should produce different hashes")
		require.NotEqual(t, h1, h3, "different robot names should produce different hashes")
	})
}

func TestVerifyRobotCredentials(t *testing.T) {
	robotName := "robot$satellite+edge-01"
	secret := "correct-secret"
	storedHash := hashRobotCredentials(robotName, secret)

	tests := []struct {
		name      string
		robotName string
		secret    string
		want      bool
	}{
		{
			name:      "correct credentials",
			robotName: robotName,
			secret:    secret,
			want:      true,
		},
		{
			name:      "wrong secret",
			robotName: robotName,
			secret:    "wrong-secret",
			want:      false,
		},
		{
			name:      "wrong robot name",
			robotName: "robot$satellite+other",
			secret:    secret,
			want:      false,
		},
		{
			name:      "both wrong",
			robotName: "robot$satellite+other",
			secret:    "wrong-secret",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifyRobotCredentials(tt.robotName, tt.secret, storedHash)
			require.Equal(t, tt.want, got)
		})
	}
}
