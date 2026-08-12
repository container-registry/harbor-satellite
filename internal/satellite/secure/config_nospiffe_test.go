//go:build nospiffe

package secure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/satellite/identity"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

const testSecret = "hunter2-harbor-robot-secret"

func newNoSpiffeEncryptor() *ConfigEncryptor {
	return NewConfigEncryptor(crypto.NewDefaultProvider(), identity.NewMockDeviceIdentity())
}

// Before the stub was made to fail closed, this produced a file that carried
// the encrypted-config version header, satisfied IsEncrypted, and held the
// credentials in plainly readable form. Encryption must now fail loudly
// instead.
func TestConfigEncryptor_NoSpiffeRefusesToEncrypt(t *testing.T) {
	e := newNoSpiffeEncryptor()

	t.Run("EncryptConfig reports the provider is unavailable", func(t *testing.T) {
		out, err := e.EncryptConfig(testConfig{Username: "admin", Password: testSecret, Token: "tok"})
		require.Error(t, err)
		require.ErrorIs(t, err, crypto.ErrCryptoUnavailable, "the cause must reach the caller")
		require.NotContains(t, string(out), testSecret, "no credential may be returned in readable form")
	})

	t.Run("EncryptToFile leaves no file behind", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")

		err := e.EncryptToFile(path, testConfig{Username: "admin", Password: testSecret, Token: "tok"})
		require.Error(t, err)
		require.ErrorIs(t, err, crypto.ErrCryptoUnavailable)

		_, statErr := os.Stat(path)
		require.True(t, os.IsNotExist(statErr), "a config that could not be encrypted must not be written at all")
	})

	t.Run("DecryptBytes reports the provider is unavailable", func(t *testing.T) {
		_, err := e.DecryptBytes([]byte(`{"version":1,"salt":"AAAA","data":"AAAA"}`))
		require.Error(t, err)
		require.ErrorIs(t, err, crypto.ErrCryptoUnavailable)
	})
}

// Guards the specific shape of the old bug: an on-disk file that looks
// encrypted to IsEncrypted while carrying readable secrets.
func TestConfigEncryptor_NoSpiffeNeverWritesFakeEncryptedFile(t *testing.T) {
	e := newNoSpiffeEncryptor()

	out, err := e.EncryptConfig(testConfig{Username: "admin", Password: testSecret, Token: "tok"})
	require.Error(t, err)

	if len(out) > 0 {
		require.False(t, IsEncrypted(out), "output must never pass as an encrypted config when nothing was encrypted")
		require.NotContains(t, strings.ToLower(string(out)), strings.ToLower(testSecret))
	}
}
