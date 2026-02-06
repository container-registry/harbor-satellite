package crypto

import (
	"bytes"
	"crypto"
)

// MockProvider implements Provider interface for testing.
type MockProvider struct {
	EncryptFunc      func(plaintext, key []byte) ([]byte, error)
	DecryptFunc      func(ciphertext, key []byte) ([]byte, error)
	DeriveKeyFunc    func(input, salt []byte, keyLen int) ([]byte, error)
	SignFunc         func(data []byte, key crypto.PrivateKey) ([]byte, error)
	VerifyFunc       func(data, signature []byte, key crypto.PublicKey) error
	GenerateFunc     func() (crypto.PrivateKey, crypto.PublicKey, error)
	HashFunc         func(data []byte) []byte
	RandomBytesFunc  func(n int) ([]byte, error)
	Err              error
	EncryptedPrefix  []byte
}

// NewMockProvider creates a MockProvider with sensible defaults.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		EncryptedPrefix: []byte("encrypted:"),
	}
}

func (m *MockProvider) Encrypt(plaintext, key []byte) ([]byte, error) {
	if m.EncryptFunc != nil {
		return m.EncryptFunc(plaintext, key)
	}
	if m.Err != nil {
		return nil, m.Err
	}
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}
	result := make([]byte, 0, len(m.EncryptedPrefix)+len(plaintext))
	result = append(result, m.EncryptedPrefix...)
	result = append(result, plaintext...)
	return result, nil
}

func (m *MockProvider) Decrypt(ciphertext, key []byte) ([]byte, error) {
	if m.DecryptFunc != nil {
		return m.DecryptFunc(ciphertext, key)
	}
	if m.Err != nil {
		return nil, m.Err
	}
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}
	if !bytes.HasPrefix(ciphertext, m.EncryptedPrefix) {
		return nil, ErrDecryptionFailed
	}
	return ciphertext[len(m.EncryptedPrefix):], nil
}

func (m *MockProvider) DeriveKey(input, salt []byte, keyLen int) ([]byte, error) {
	if m.DeriveKeyFunc != nil {
		return m.DeriveKeyFunc(input, salt, keyLen)
	}
	if m.Err != nil {
		return nil, m.Err
	}
	if len(input) == 0 {
		return nil, ErrInvalidInput
	}
	if keyLen <= 0 {
		return nil, ErrInvalidKeyLength
	}
	key := make([]byte, keyLen)
	for i := range key {
		if i < len(input) {
			key[i] = input[i]
		}
		if i < len(salt) {
			key[i] ^= salt[i]
		}
	}
	return key, nil
}

func (m *MockProvider) Sign(data []byte, key crypto.PrivateKey) ([]byte, error) {
	if m.SignFunc != nil {
		return m.SignFunc(data, key)
	}
	if m.Err != nil {
		return nil, m.Err
	}
	sig := append([]byte("sig:"), m.Hash(data)...)
	return sig, nil
}

func (m *MockProvider) Verify(data, signature []byte, key crypto.PublicKey) error {
	if m.VerifyFunc != nil {
		return m.VerifyFunc(data, signature, key)
	}
	if m.Err != nil {
		return m.Err
	}
	expected := append([]byte("sig:"), m.Hash(data)...)
	if !bytes.Equal(signature, expected) {
		return ErrSignatureMismatch
	}
	return nil
}

func (m *MockProvider) GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc()
	}
	if m.Err != nil {
		return nil, nil, m.Err
	}
	return []byte("mock-private-key"), []byte("mock-public-key"), nil
}

func (m *MockProvider) Hash(data []byte) []byte {
	if m.HashFunc != nil {
		return m.HashFunc(data)
	}
	h := make([]byte, 32)
	for i, b := range data {
		h[i%32] ^= b
	}
	return h
}

func (m *MockProvider) RandomBytes(n int) ([]byte, error) {
	if m.RandomBytesFunc != nil {
		return m.RandomBytesFunc(n)
	}
	if m.Err != nil {
		return nil, m.Err
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b, nil
}
