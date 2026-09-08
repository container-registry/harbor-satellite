package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockProvider_Encrypt(t *testing.T) {
	tests := []struct {
		name        string
		plaintext   []byte
		key         []byte
		setupMock   func(*MockProvider)
		expectErr   error
		checkResult func(t *testing.T, result []byte)
	}{
		{
			name:      "encrypt success",
			plaintext: []byte("secret data"),
			key:       []byte("test-key"),
			checkResult: func(t *testing.T, result []byte) {
				require.True(t, bytes.HasPrefix(result, []byte("encrypted:")))
				require.Contains(t, string(result), "secret data")
			},
		},
		{
			name:      "encrypt empty key fails",
			plaintext: []byte("secret data"),
			key:       []byte{},
			expectErr: ErrInvalidKey,
		},
		{
			name:      "encrypt empty plaintext succeeds",
			plaintext: []byte{},
			key:       []byte("test-key"),
			checkResult: func(t *testing.T, result []byte) {
				require.Equal(t, []byte("encrypted:"), result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMockProvider()
			if tt.setupMock != nil {
				tt.setupMock(m)
			}

			result, err := m.Encrypt(tt.plaintext, tt.key)

			if tt.expectErr != nil {
				require.ErrorIs(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestMockProvider_Decrypt(t *testing.T) {
	tests := []struct {
		name        string
		ciphertext  []byte
		key         []byte
		expectErr   error
		checkResult func(t *testing.T, result []byte)
	}{
		{
			name:       "decrypt success",
			ciphertext: []byte("encrypted:secret data"),
			key:        []byte("test-key"),
			checkResult: func(t *testing.T, result []byte) {
				require.Equal(t, []byte("secret data"), result)
			},
		},
		{
			name:       "decrypt wrong key fails",
			ciphertext: []byte("encrypted:secret data"),
			key:        []byte{},
			expectErr:  ErrInvalidKey,
		},
		{
			name:       "decrypt corrupted data fails",
			ciphertext: []byte("not-encrypted-data"),
			key:        []byte("test-key"),
			expectErr:  ErrDecryptionFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMockProvider()

			result, err := m.Decrypt(tt.ciphertext, tt.key)

			if tt.expectErr != nil {
				require.ErrorIs(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestMockProvider_EncryptDecryptRoundtrip(t *testing.T) {
	m := NewMockProvider()
	plaintext := []byte("test secret configuration data")
	key := []byte("encryption-key-32bytes-long-xxx")

	encrypted, err := m.Encrypt(plaintext, key)
	require.NoError(t, err)
	require.NotEqual(t, plaintext, encrypted)

	decrypted, err := m.Decrypt(encrypted, key)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}

func TestMockProvider_EncryptedNotReadableAsPlaintext(t *testing.T) {
	m := NewMockProvider()
	plaintext := []byte(`{"username":"admin","password":"secret"}`)
	key := []byte("encryption-key")

	encrypted, err := m.Encrypt(plaintext, key)
	require.NoError(t, err)

	require.NotEqual(t, plaintext, encrypted)
	require.True(t, bytes.HasPrefix(encrypted, []byte("encrypted:")))
}

func TestMockProvider_DeriveKey(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		salt      []byte
		keyLen    int
		expectErr error
	}{
		{
			name:   "derive key from fingerprint",
			input:  []byte("device-fingerprint-abc123"),
			salt:   []byte("random-salt"),
			keyLen: 32,
		},
		{
			name:      "derive key empty input fails",
			input:     []byte{},
			salt:      []byte("salt"),
			keyLen:    32,
			expectErr: ErrInvalidInput,
		},
		{
			name:      "derive key invalid length fails",
			input:     []byte("input"),
			salt:      []byte("salt"),
			keyLen:    0,
			expectErr: ErrInvalidKeyLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMockProvider()

			result, err := m.DeriveKey(tt.input, tt.salt, tt.keyLen)

			if tt.expectErr != nil {
				require.ErrorIs(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, result, tt.keyLen)
		})
	}
}

func TestMockProvider_DeriveKeyDeterministic(t *testing.T) {
	m := NewMockProvider()
	input := []byte("same-device-fingerprint")
	salt := []byte("same-salt")

	key1, err := m.DeriveKey(input, salt, 32)
	require.NoError(t, err)

	key2, err := m.DeriveKey(input, salt, 32)
	require.NoError(t, err)

	require.Equal(t, key1, key2, "same input should produce same key")
}

func TestMockProvider_DeriveKeyDifferentInputs(t *testing.T) {
	m := NewMockProvider()

	key1, err := m.DeriveKey([]byte("fingerprint-1"), []byte("salt"), 32)
	require.NoError(t, err)

	key2, err := m.DeriveKey([]byte("fingerprint-2"), []byte("salt"), 32)
	require.NoError(t, err)

	require.NotEqual(t, key1, key2, "different inputs should produce different keys")
}

func TestMockProvider_Hash(t *testing.T) {
	m := NewMockProvider()

	data := []byte("test data to hash")
	hash := m.Hash(data)

	require.Len(t, hash, 32)

	hash2 := m.Hash(data)
	require.Equal(t, hash, hash2, "hash should be deterministic")
}

func TestMockProvider_RandomBytes(t *testing.T) {
	m := NewMockProvider()

	bytes16, err := m.RandomBytes(16)
	require.NoError(t, err)
	require.Len(t, bytes16, 16)

	bytes32, err := m.RandomBytes(32)
	require.NoError(t, err)
	require.Len(t, bytes32, 32)
}

func TestMockProvider_SignVerify(t *testing.T) {
	m := NewMockProvider()
	data := []byte("data to sign")

	sig, err := m.Sign(data, []byte("private-key"))
	require.NoError(t, err)
	require.NotEmpty(t, sig)

	err = m.Verify(data, sig, []byte("public-key"))
	require.NoError(t, err)
}

func TestMockProvider_VerifyInvalidSignature(t *testing.T) {
	m := NewMockProvider()
	data := []byte("data to sign")

	err := m.Verify(data, []byte("invalid-signature"), []byte("public-key"))
	require.ErrorIs(t, err, ErrSignatureMismatch)
}

func TestMockProvider_GenerateKeyPair(t *testing.T) {
	m := NewMockProvider()

	priv, pub, err := m.GenerateKeyPair()
	require.NoError(t, err)
	require.NotNil(t, priv)
	require.NotNil(t, pub)
}

func TestMockProvider_ReEncryptionWithNewKey(t *testing.T) {
	m := NewMockProvider()
	plaintext := []byte("config data")
	oldKey := []byte("old-key")
	newKey := []byte("new-key")

	encrypted, err := m.Encrypt(plaintext, oldKey)
	require.NoError(t, err)

	decrypted, err := m.Decrypt(encrypted, oldKey)
	require.NoError(t, err)

	reencrypted, err := m.Encrypt(decrypted, newKey)
	require.NoError(t, err)

	finalDecrypted, err := m.Decrypt(reencrypted, newKey)
	require.NoError(t, err)
	require.Equal(t, plaintext, finalDecrypted)
}
