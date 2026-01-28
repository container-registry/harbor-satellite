package secure

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/crypto"
	"github.com/container-registry/harbor-satellite/internal/identity"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

func newTestEncryptor() *ConfigEncryptor {
	return NewConfigEncryptor(
		crypto.NewAESProvider(),
		identity.NewMockDeviceIdentity(),
	)
}

func TestConfigEncryptor_EncryptConfig(t *testing.T) {
	e := newTestEncryptor()

	t.Run("encrypt success", func(t *testing.T) {
		cfg := testConfig{
			Username: "admin",
			Password: "secret123",
			Token:    "token-xyz",
		}

		encrypted, err := e.EncryptConfig(cfg)
		require.NoError(t, err)
		require.NotEmpty(t, encrypted)
	})

	t.Run("encrypt empty config succeeds", func(t *testing.T) {
		cfg := testConfig{}
		encrypted, err := e.EncryptConfig(cfg)
		require.NoError(t, err)
		require.NotEmpty(t, encrypted)
	})
}

func TestConfigEncryptor_EncryptEmptyKeyFails(t *testing.T) {
	mockCrypto := crypto.NewMockProvider()
	mockCrypto.Err = crypto.ErrInvalidKey

	e := NewConfigEncryptor(mockCrypto, identity.NewMockDeviceIdentity())

	cfg := testConfig{Username: "test"}
	_, err := e.EncryptConfig(cfg)
	require.Error(t, err)
}

func TestConfigEncryptor_DecryptConfig(t *testing.T) {
	e := newTestEncryptor()

	t.Run("decrypt success", func(t *testing.T) {
		original := testConfig{
			Username: "admin",
			Password: "secret123",
			Token:    "token-xyz",
		}

		encrypted, err := e.EncryptConfig(original)
		require.NoError(t, err)

		var decrypted testConfig
		err = e.DecryptConfig(encrypted, &decrypted)
		require.NoError(t, err)
		require.Equal(t, original, decrypted)
	})

	t.Run("decrypt corrupted data fails", func(t *testing.T) {
		var cfg testConfig
		err := e.DecryptConfig([]byte("not valid json"), &cfg)
		require.ErrorIs(t, err, ErrConfigCorrupted)
	})

	t.Run("decrypt wrong device fails", func(t *testing.T) {
		original := testConfig{Username: "test"}
		encrypted, err := e.EncryptConfig(original)
		require.NoError(t, err)

		differentDevice := identity.NewMockDeviceIdentity()
		differentDevice.FingerprintValue = "different-fingerprint"

		e2 := NewConfigEncryptor(crypto.NewAESProvider(), differentDevice)

		var decrypted testConfig
		err = e2.DecryptConfig(encrypted, &decrypted)
		require.ErrorIs(t, err, ErrDecryptionFailed)
	})
}

func TestConfigEncryptor_EncryptDecryptRoundtrip(t *testing.T) {
	e := newTestEncryptor()

	original := testConfig{
		Username: "admin",
		Password: "super-secret-password",
		Token:    "jwt-token-here",
	}

	encrypted, err := e.EncryptConfig(original)
	require.NoError(t, err)

	var decrypted testConfig
	err = e.DecryptConfig(encrypted, &decrypted)
	require.NoError(t, err)

	require.Equal(t, original, decrypted)
}

func TestConfigEncryptor_EncryptedNotReadableAsPlaintext(t *testing.T) {
	e := newTestEncryptor()

	cfg := testConfig{
		Username: "admin",
		Password: "secret123",
	}

	encrypted, err := e.EncryptConfig(cfg)
	require.NoError(t, err)

	require.NotContains(t, string(encrypted), "admin")
	require.NotContains(t, string(encrypted), "secret123")
}

func TestConfigEncryptor_ReEncryptionWithNewSalt(t *testing.T) {
	e := newTestEncryptor()

	original := testConfig{Username: "test", Password: "pass"}

	encrypted1, err := e.EncryptConfig(original)
	require.NoError(t, err)

	reencrypted, err := e.ReEncrypt(encrypted1)
	require.NoError(t, err)

	require.NotEqual(t, encrypted1, reencrypted)

	var decrypted testConfig
	err = e.DecryptConfig(reencrypted, &decrypted)
	require.NoError(t, err)
	require.Equal(t, original, decrypted)
}

func TestConfigEncryptor_EncryptToFile(t *testing.T) {
	e := newTestEncryptor()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.enc")

	cfg := testConfig{Username: "admin", Password: "secret"}

	err := e.EncryptToFile(path, cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestConfigEncryptor_DecryptFromFile(t *testing.T) {
	e := newTestEncryptor()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.enc")

	original := testConfig{Username: "admin", Password: "secret"}

	err := e.EncryptToFile(path, original)
	require.NoError(t, err)

	var decrypted testConfig
	err = e.DecryptFromFile(path, &decrypted)
	require.NoError(t, err)
	require.Equal(t, original, decrypted)
}

func TestConfigEncryptor_DecryptFromFileNotFound(t *testing.T) {
	e := newTestEncryptor()

	var cfg testConfig
	err := e.DecryptFromFile("/nonexistent/path", &cfg)
	require.ErrorIs(t, err, ErrConfigNotFound)
}

func TestIsEncrypted(t *testing.T) {
	e := newTestEncryptor()

	t.Run("returns true for encrypted config", func(t *testing.T) {
		cfg := testConfig{Username: "test"}
		encrypted, err := e.EncryptConfig(cfg)
		require.NoError(t, err)

		require.True(t, IsEncrypted(encrypted))
	})

	t.Run("returns false for plaintext", func(t *testing.T) {
		plaintext := []byte(`{"username":"admin","password":"secret"}`)
		require.False(t, IsEncrypted(plaintext))
	})

	t.Run("returns false for invalid json", func(t *testing.T) {
		require.False(t, IsEncrypted([]byte("not json")))
	})
}

func TestConfigEncryptor_DeriveKeyDeterministic(t *testing.T) {
	device := identity.NewMockDeviceIdentity()
	e1 := NewConfigEncryptor(crypto.NewAESProvider(), device)
	e2 := NewConfigEncryptor(crypto.NewAESProvider(), device)

	cfg := testConfig{Username: "test"}

	encrypted1, err := e1.EncryptConfig(cfg)
	require.NoError(t, err)

	var decrypted testConfig
	err = e2.DecryptConfig(encrypted1, &decrypted)
	require.NoError(t, err)
	require.Equal(t, cfg, decrypted)
}

func TestConfigEncryptor_KeyDeriveFails(t *testing.T) {
	mockDevice := identity.NewMockDeviceIdentity()
	mockDevice.FingerprintErr = identity.ErrFingerprintFailed

	e := NewConfigEncryptor(crypto.NewAESProvider(), mockDevice)

	cfg := testConfig{Username: "test"}
	_, err := e.EncryptConfig(cfg)
	require.ErrorIs(t, err, ErrKeyDeriveFailed)
}

func TestConfigEncryptor_UniqueEncryption(t *testing.T) {
	e := newTestEncryptor()
	cfg := testConfig{Username: "same", Password: "same"}

	enc1, err := e.EncryptConfig(cfg)
	require.NoError(t, err)

	enc2, err := e.EncryptConfig(cfg)
	require.NoError(t, err)

	require.NotEqual(t, enc1, enc2, "each encryption should be unique due to random salt")
}
