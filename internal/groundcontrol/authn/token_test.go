package authn_test

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingSessions = "Ground Control issues no satellite sessions; every beat verifies the robot secret"

func TestNewSessionToken_FormatAndHash(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	token, hash, err := authn.NewSessionToken()

	require.NoError(t, err)
	require.Len(t, token, 47)
	require.True(t, strings.HasPrefix(token, authn.SessionTokenPrefix), token)
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, authn.SessionTokenPrefix))
	require.NoError(t, err)
	require.Len(t, raw, 32)
	require.Equal(t, sha256.Sum256([]byte(token)), hash)
	again, err := authn.HashSessionToken(token)
	require.NoError(t, err)
	require.Equal(t, hash, again)
	other, _, err := authn.NewSessionToken()
	require.NoError(t, err)
	require.NotEqual(t, token, other)
}

func TestHashSessionToken_RejectsForeignTokens(t *testing.T) {
	pending.Plan(t, "4.1", pendingSessions)
	valid, _, err := authn.NewSessionToken()
	require.NoError(t, err)
	for name, token := range map[string]string{
		"empty":          "",
		"robot secret":   "Abcdefgh1234567890abcdefgh123456",
		"user session":   strings.Repeat("a", 64),
		"prefix only":    authn.SessionTokenPrefix,
		"one byte short": valid[:len(valid)-1],
		"one byte long":  valid + "A",
		"wrong prefix":   "hsx_" + strings.TrimPrefix(valid, authn.SessionTokenPrefix),
		"not base64url":  authn.SessionTokenPrefix + strings.Repeat("*", 43),
	} {
		_, err := authn.HashSessionToken(token)
		require.ErrorIs(t, err, fleet.ErrSessionInvalid, name)
	}
}
