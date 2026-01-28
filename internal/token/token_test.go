package token

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateToken(t *testing.T) {
	t.Run("generates valid token", func(t *testing.T) {
		token, encoded, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)
		require.NotNil(t, token)
		require.NotEmpty(t, encoded)
		require.Equal(t, TokenVersion, token.Version)
		require.NotEmpty(t, token.ID)
		require.Equal(t, "https://gc.example.com", token.GroundURL)
	})

	t.Run("tokens are unique", func(t *testing.T) {
		_, enc1, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)

		_, enc2, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)

		require.NotEqual(t, enc1, enc2)
	})

	t.Run("expiry is set correctly", func(t *testing.T) {
		token, _, err := GenerateToken("https://gc.example.com", 2*time.Hour)
		require.NoError(t, err)

		expectedExpiry := time.Now().Add(2 * time.Hour)
		require.WithinDuration(t, expectedExpiry, token.ExpiresAt, time.Second)
	})
}

func TestDecodeToken(t *testing.T) {
	t.Run("decodes valid token", func(t *testing.T) {
		original, encoded, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)

		decoded, err := DecodeToken(encoded)
		require.NoError(t, err)
		require.Equal(t, original.Version, decoded.Version)
		require.Equal(t, original.ID, decoded.ID)
		require.Equal(t, original.GroundURL, decoded.GroundURL)
	})

	t.Run("rejects empty token", func(t *testing.T) {
		_, err := DecodeToken("")
		require.ErrorIs(t, err, ErrTokenMalformed)
	})

	t.Run("rejects malformed base64", func(t *testing.T) {
		_, err := DecodeToken("not-valid-base64!!!")
		require.ErrorIs(t, err, ErrTokenMalformed)
	})

	t.Run("rejects invalid json", func(t *testing.T) {
		_, err := DecodeToken("bm90LWpzb24")
		require.ErrorIs(t, err, ErrTokenMalformed)
	})

	t.Run("rejects token with missing fields", func(t *testing.T) {
		_, err := DecodeToken("eyJ2IjowfQ") // {"v":0}
		require.ErrorIs(t, err, ErrTokenMalformed)
	})
}

func TestJoinToken_Validate(t *testing.T) {
	t.Run("valid token accepted", func(t *testing.T) {
		token, _, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)

		err = token.Validate()
		require.NoError(t, err)
	})

	t.Run("expired token rejected", func(t *testing.T) {
		token := &JoinToken{
			Version:   TokenVersion,
			ID:        "test-id",
			ExpiresAt: time.Now().Add(-time.Hour),
		}

		err := token.Validate()
		require.ErrorIs(t, err, ErrTokenExpired)
	})

	t.Run("invalid version rejected", func(t *testing.T) {
		token := &JoinToken{
			Version:   999,
			ID:        "test-id",
			ExpiresAt: time.Now().Add(time.Hour),
		}

		err := token.Validate()
		require.ErrorIs(t, err, ErrTokenInvalid)
	})

	t.Run("empty ID rejected", func(t *testing.T) {
		token := &JoinToken{
			Version:   TokenVersion,
			ID:        "",
			ExpiresAt: time.Now().Add(time.Hour),
		}

		err := token.Validate()
		require.ErrorIs(t, err, ErrTokenMalformed)
	})
}

func TestJoinToken_ValidateForGroundControl(t *testing.T) {
	t.Run("accepts token for matching ground control", func(t *testing.T) {
		token, _, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)

		err = token.ValidateForGroundControl("https://gc.example.com")
		require.NoError(t, err)
	})

	t.Run("rejects token for wrong ground control", func(t *testing.T) {
		token, _, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)

		err = token.ValidateForGroundControl("https://other.example.com")
		require.ErrorIs(t, err, ErrTokenInvalid)
	})

	t.Run("accepts token without ground control URL", func(t *testing.T) {
		token := &JoinToken{
			Version:   TokenVersion,
			ID:        "test-id",
			ExpiresAt: time.Now().Add(time.Hour),
			GroundURL: "",
		}

		err := token.ValidateForGroundControl("https://any.example.com")
		require.NoError(t, err)
	})
}

func TestJoinToken_IsExpired(t *testing.T) {
	t.Run("returns false for valid token", func(t *testing.T) {
		token, _, err := GenerateToken("https://gc.example.com", time.Hour)
		require.NoError(t, err)
		require.False(t, token.IsExpired())
	})

	t.Run("returns true for expired token", func(t *testing.T) {
		token := &JoinToken{
			Version:   TokenVersion,
			ID:        "test-id",
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		require.True(t, token.IsExpired())
	})
}

func TestJoinToken_TimeToExpiry(t *testing.T) {
	token, _, err := GenerateToken("https://gc.example.com", time.Hour)
	require.NoError(t, err)

	ttl := token.TimeToExpiry()
	require.True(t, ttl > 59*time.Minute)
	require.True(t, ttl <= time.Hour)
}

func TestJoinToken_EncodeDecode(t *testing.T) {
	original := &JoinToken{
		Version:   TokenVersion,
		ID:        "unique-token-id",
		ExpiresAt: time.Now().Add(24 * time.Hour).Truncate(time.Second),
		GroundURL: "https://gc.example.com",
	}

	encoded, err := original.Encode()
	require.NoError(t, err)

	decoded, err := DecodeToken(encoded)
	require.NoError(t, err)

	require.Equal(t, original.Version, decoded.Version)
	require.Equal(t, original.ID, decoded.ID)
	require.Equal(t, original.GroundURL, decoded.GroundURL)
	require.WithinDuration(t, original.ExpiresAt, decoded.ExpiresAt, time.Second)
}
