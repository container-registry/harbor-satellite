package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAESProvider_Encrypt(t *testing.T) {
	p := NewAESProvider()

	tests := []struct {
		name      string
		plaintext []byte
		key       []byte
		expectErr error
	}{
		{
			name:      "encrypt success",
			plaintext: []byte("secret data"),
			key:       []byte("test-key-32-bytes-long-xxxxxxxx"),
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
		},
		{
			name:      "encrypt with short key succeeds",
			plaintext: []byte("data"),
			key:       []byte("short"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Encrypt(tt.plaintext, tt.key)

			if tt.expectErr != nil {
				require.ErrorIs(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, result)
			require.NotEqual(t, tt.plaintext, result)
		})
	}
}

func TestAESProvider_Decrypt(t *testing.T) {
	p := NewAESProvider()
	key := []byte("test-key")
	plaintext := []byte("test data")

	encrypted, err := p.Encrypt(plaintext, key)
	require.NoError(t, err)

	t.Run("decrypt success", func(t *testing.T) {
		result, err := p.Decrypt(encrypted, key)
		require.NoError(t, err)
		require.Equal(t, plaintext, result)
	})

	t.Run("decrypt empty key fails", func(t *testing.T) {
		_, err := p.Decrypt(encrypted, []byte{})
		require.ErrorIs(t, err, ErrInvalidKey)
	})

	t.Run("decrypt wrong key fails", func(t *testing.T) {
		_, err := p.Decrypt(encrypted, []byte("wrong-key"))
		require.ErrorIs(t, err, ErrDecryptionFailed)
	})

	t.Run("decrypt corrupted data fails", func(t *testing.T) {
		_, err := p.Decrypt([]byte("short"), key)
		require.ErrorIs(t, err, ErrDecryptionFailed)
	})

	t.Run("decrypt tampered data fails", func(t *testing.T) {
		tampered := make([]byte, len(encrypted))
		copy(tampered, encrypted)
		tampered[len(tampered)-1] ^= 0xff
		_, err := p.Decrypt(tampered, key)
		require.ErrorIs(t, err, ErrDecryptionFailed)
	})
}

func TestAESProvider_EncryptDecryptRoundtrip(t *testing.T) {
	p := NewAESProvider()
	testCases := [][]byte{
		[]byte("simple text"),
		[]byte(`{"username":"admin","password":"secret123"}`),
		make([]byte, 1024),
		make([]byte, 64*1024),
	}

	key := []byte("encryption-key-for-testing")

	for i, plaintext := range testCases {
		encrypted, err := p.Encrypt(plaintext, key)
		require.NoError(t, err, "case %d encrypt", i)

		decrypted, err := p.Decrypt(encrypted, key)
		require.NoError(t, err, "case %d decrypt", i)

		require.Equal(t, plaintext, decrypted, "case %d roundtrip", i)
	}
}

func TestAESProvider_EncryptDecryptEmptyRoundtrip(t *testing.T) {
	p := NewAESProvider()
	key := []byte("encryption-key")
	plaintext := []byte{}

	encrypted, err := p.Encrypt(plaintext, key)
	require.NoError(t, err)

	decrypted, err := p.Decrypt(encrypted, key)
	require.NoError(t, err)
	require.Len(t, decrypted, 0)
}

func TestAESProvider_EncryptedNotReadableAsPlaintext(t *testing.T) {
	p := NewAESProvider()
	plaintext := []byte(`{"username":"admin","password":"secret"}`)
	key := []byte("encryption-key")

	encrypted, err := p.Encrypt(plaintext, key)
	require.NoError(t, err)

	require.NotEqual(t, plaintext, encrypted)
	require.False(t, bytes.Contains(encrypted, []byte("admin")))
	require.False(t, bytes.Contains(encrypted, []byte("secret")))
}

func TestAESProvider_DeriveKey(t *testing.T) {
	p := NewAESProvider()

	tests := []struct {
		name      string
		input     []byte
		salt      []byte
		keyLen    int
		expectErr error
	}{
		{
			name:   "derive 32 byte key",
			input:  []byte("device-fingerprint"),
			salt:   []byte("random-salt"),
			keyLen: 32,
		},
		{
			name:   "derive 64 byte key",
			input:  []byte("input"),
			salt:   []byte("salt"),
			keyLen: 64,
		},
		{
			name:      "empty input fails",
			input:     []byte{},
			salt:      []byte("salt"),
			keyLen:    32,
			expectErr: ErrInvalidInput,
		},
		{
			name:      "invalid key length fails",
			input:     []byte("input"),
			salt:      []byte("salt"),
			keyLen:    0,
			expectErr: ErrInvalidKeyLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.DeriveKey(tt.input, tt.salt, tt.keyLen)

			if tt.expectErr != nil {
				require.ErrorIs(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, result, tt.keyLen)
		})
	}
}

func TestAESProvider_DeriveKeyDeterministic(t *testing.T) {
	p := NewAESProvider()
	input := []byte("same-device-fingerprint")
	salt := []byte("same-salt")

	key1, err := p.DeriveKey(input, salt, 32)
	require.NoError(t, err)

	key2, err := p.DeriveKey(input, salt, 32)
	require.NoError(t, err)

	require.Equal(t, key1, key2, "same input should produce same key")
}

func TestAESProvider_DeriveKeyDifferentInputs(t *testing.T) {
	p := NewAESProvider()

	key1, err := p.DeriveKey([]byte("fingerprint-1"), []byte("salt"), 32)
	require.NoError(t, err)

	key2, err := p.DeriveKey([]byte("fingerprint-2"), []byte("salt"), 32)
	require.NoError(t, err)

	require.NotEqual(t, key1, key2, "different inputs should produce different keys")
}

func TestAESProvider_DeriveKeyDifferentSalts(t *testing.T) {
	p := NewAESProvider()
	input := []byte("same-input")

	key1, err := p.DeriveKey(input, []byte("salt-1"), 32)
	require.NoError(t, err)

	key2, err := p.DeriveKey(input, []byte("salt-2"), 32)
	require.NoError(t, err)

	require.NotEqual(t, key1, key2, "different salts should produce different keys")
}

func TestAESProvider_DeriveKeyMinimumEntropy(t *testing.T) {
	p := NewAESProvider()

	// Derive a key and check it has sufficient entropy
	// A good key should have bytes distributed across the range
	key, err := p.DeriveKey([]byte("device-fingerprint-abc123"), []byte("salt"), 32)
	require.NoError(t, err)

	// Check that key is not all zeros
	allZeros := true
	for _, b := range key {
		if b != 0 {
			allZeros = false
			break
		}
	}
	require.False(t, allZeros, "key should not be all zeros")

	// Check that key is not all same byte
	allSame := true
	first := key[0]
	for _, b := range key[1:] {
		if b != first {
			allSame = false
			break
		}
	}
	require.False(t, allSame, "key should not be all same byte")

	// Check byte distribution - at least 16 unique bytes in a 32-byte key
	uniqueBytes := make(map[byte]bool)
	for _, b := range key {
		uniqueBytes[b] = true
	}
	require.GreaterOrEqual(t, len(uniqueBytes), 16, "key should have at least 16 unique bytes")
}

func TestAESProvider_Hash(t *testing.T) {
	p := NewAESProvider()

	data := []byte("test data to hash")
	hash := p.Hash(data)

	require.Len(t, hash, 32) // SHA-256

	hash2 := p.Hash(data)
	require.Equal(t, hash, hash2, "hash should be deterministic")

	differentHash := p.Hash([]byte("different data"))
	require.NotEqual(t, hash, differentHash)
}

func TestAESProvider_RandomBytes(t *testing.T) {
	p := NewAESProvider()

	bytes1, err := p.RandomBytes(32)
	require.NoError(t, err)
	require.Len(t, bytes1, 32)

	bytes2, err := p.RandomBytes(32)
	require.NoError(t, err)

	require.NotEqual(t, bytes1, bytes2, "random bytes should be unique")
}

func TestAESProvider_SignVerify(t *testing.T) {
	p := NewAESProvider()
	data := []byte("data to sign")

	priv, pub, err := p.GenerateKeyPair()
	require.NoError(t, err)

	sig, err := p.Sign(data, priv)
	require.NoError(t, err)
	require.NotEmpty(t, sig)

	err = p.Verify(data, sig, pub)
	require.NoError(t, err)
}

func TestAESProvider_VerifyInvalidSignature(t *testing.T) {
	p := NewAESProvider()
	data := []byte("data to sign")

	_, pub, err := p.GenerateKeyPair()
	require.NoError(t, err)

	err = p.Verify(data, []byte("invalid-signature"), pub)
	require.Error(t, err)
}

func TestAESProvider_VerifyTamperedData(t *testing.T) {
	p := NewAESProvider()
	data := []byte("original data")

	priv, pub, err := p.GenerateKeyPair()
	require.NoError(t, err)

	sig, err := p.Sign(data, priv)
	require.NoError(t, err)

	err = p.Verify([]byte("tampered data"), sig, pub)
	require.ErrorIs(t, err, ErrSignatureMismatch)
}

func TestAESProvider_GenerateKeyPair(t *testing.T) {
	p := NewAESProvider()

	priv, pub, err := p.GenerateKeyPair()
	require.NoError(t, err)
	require.NotNil(t, priv)
	require.NotNil(t, pub)

	priv2, pub2, err := p.GenerateKeyPair()
	require.NoError(t, err)
	require.NotEqual(t, priv, priv2, "keys should be unique")
	require.NotEqual(t, pub, pub2, "keys should be unique")
}

func TestAESProvider_ReEncryptionWithNewKey(t *testing.T) {
	p := NewAESProvider()
	plaintext := []byte("config data to re-encrypt")
	oldKey := []byte("old-encryption-key")
	newKey := []byte("new-encryption-key")

	encrypted, err := p.Encrypt(plaintext, oldKey)
	require.NoError(t, err)

	decrypted, err := p.Decrypt(encrypted, oldKey)
	require.NoError(t, err)

	reencrypted, err := p.Encrypt(decrypted, newKey)
	require.NoError(t, err)

	finalDecrypted, err := p.Decrypt(reencrypted, newKey)
	require.NoError(t, err)
	require.Equal(t, plaintext, finalDecrypted)
}

func TestAESProvider_UniqueEncryption(t *testing.T) {
	p := NewAESProvider()
	plaintext := []byte("same plaintext")
	key := []byte("same key")

	enc1, err := p.Encrypt(plaintext, key)
	require.NoError(t, err)

	enc2, err := p.Encrypt(plaintext, key)
	require.NoError(t, err)

	require.NotEqual(t, enc1, enc2, "each encryption should be unique due to random nonce")
}
