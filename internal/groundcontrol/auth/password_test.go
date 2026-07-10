package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyPassword(t *testing.T) {
	password := "test-password-123"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{
			name:     "correct password",
			password: password,
			hash:     hash,
			want:     true,
		},
		{
			name:     "wrong password",
			password: "wrong-password",
			hash:     hash,
			want:     false,
		},
		{
			name:     "empty password",
			password: "",
			hash:     hash,
			want:     false,
		},
		{
			name:     "malformed hash",
			password: password,
			hash:     "not-a-valid-hash",
			want:     false,
		},
		{
			name:     "empty hash",
			password: password,
			hash:     "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyPassword(tt.password, tt.hash)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{
			name:     "normal password",
			password: "MySecurePassword123!",
		},
		{
			name:     "empty password",
			password: "",
		},
		{
			name:     "long password",
			password: "a very long password that exceeds typical password length requirements for testing purposes",
		},
		{
			name:     "special characters",
			password: "p@$$w0rd!#%&*()[]{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)
			require.NoError(t, err)
			require.NotEmpty(t, hash)
			require.Contains(t, hash, "$argon2id$")

			// Verify the hash works
			valid := VerifyPassword(tt.password, hash)
			require.True(t, valid)
		})
	}
}

func TestHashPassword_UniqueHashes(t *testing.T) {
	password := "same-password"
	hash1, err := HashPassword(password)
	require.NoError(t, err)

	hash2, err := HashPassword(password)
	require.NoError(t, err)

	// Same password should produce different hashes due to random salt
	require.NotEqual(t, hash1, hash2)

	// Both should verify correctly
	require.True(t, VerifyPassword(password, hash1))
	require.True(t, VerifyPassword(password, hash2))
}

func TestGenerateSessionToken(t *testing.T) {
	token1, err := GenerateSessionToken()
	require.NoError(t, err)
	require.NotEmpty(t, token1)

	token2, err := GenerateSessionToken()
	require.NoError(t, err)
	require.NotEmpty(t, token2)

	// Tokens should be unique
	require.NotEqual(t, token1, token2)

	// Tokens should be base64-encoded (URL encoding allows =, -, _)
	require.Regexp(t, `^[A-Za-z0-9_=-]+$`, token1)
	require.Regexp(t, `^[A-Za-z0-9_=-]+$`, token2)
}
