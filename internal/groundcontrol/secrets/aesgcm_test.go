package secrets_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/secrets"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingSealer = "successor robot secrets are not sealed; there is no rotation"

func newSealer(t *testing.T, key byte) *secrets.AESGCM {
	t.Helper()
	s, err := secrets.NewAESGCM(bytes.Repeat([]byte{key}, 32))
	require.NoError(t, err)
	return s
}

func TestAESGCM_SealOpenRoundTrip(t *testing.T) {
	pending.Plan(t, "8.4", pendingSealer)
	s := newSealer(t, 1)

	sealed, err := s.Seal([]byte("robot secret"))
	require.NoError(t, err)
	again, err := s.Seal([]byte("robot secret"))
	require.NoError(t, err)

	require.NotContains(t, string(sealed), "robot secret")
	require.NotEqual(t, sealed, again, "a fresh nonce per seal")
	opened, err := s.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "robot secret", string(opened))
}

func TestAESGCM_RejectsTamperedOrWrongKey(t *testing.T) {
	pending.Plan(t, "8.4", pendingSealer)
	s := newSealer(t, 1)
	sealed, err := s.Seal([]byte("robot secret"))
	require.NoError(t, err)

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	_, err = s.Open(tampered)
	require.Error(t, err)
	_, err = newSealer(t, 2).Open(sealed)
	require.Error(t, err)
	_, err = s.Open(sealed[:3])
	require.Error(t, err, "shorter than version and nonce")
}

func TestParseKey_Requires32Bytes(t *testing.T) {
	pending.Plan(t, "8.4", pendingSealer)
	key, err := secrets.ParseKey(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	require.NoError(t, err)
	require.Len(t, key, 32)
	for _, in := range []string{"", "not base64!", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		_, err := secrets.ParseKey(in)
		require.Error(t, err, "%q", in)
	}
}
