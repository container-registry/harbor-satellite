//go:build nospiffe

package crypto

// NoOpProvider provides no-op cryptographic operations for minimal builds.
// Data passes through unchanged - NOT for production use with sensitive data.
type NoOpProvider struct{}

func NewNoOpProvider() *NoOpProvider {
	return &NoOpProvider{}
}

func NewAESProvider() *NoOpProvider {
	return &NoOpProvider{}
}

func (p *NoOpProvider) Encrypt(plaintext, _ []byte) ([]byte, error) {
	return plaintext, nil
}

func (p *NoOpProvider) Decrypt(ciphertext, _ []byte) ([]byte, error) {
	return ciphertext, nil
}

func (p *NoOpProvider) DeriveKey(input, _ []byte, keyLen int) ([]byte, error) {
	if len(input) >= keyLen {
		return input[:keyLen], nil
	}
	result := make([]byte, keyLen)
	copy(result, input)
	return result, nil
}

func (p *NoOpProvider) Sign(_ []byte, _ any) ([]byte, error) {
	return []byte("nospiffe-stub-signature"), nil
}

func (p *NoOpProvider) Verify(_, _ []byte, _ any) error {
	return nil
}

func (p *NoOpProvider) GenerateKeyPair() (any, any, error) {
	return nil, nil, nil
}

func (p *NoOpProvider) Hash(data []byte) []byte {
	return data
}

func (p *NoOpProvider) RandomBytes(n int) ([]byte, error) {
	return make([]byte, n), nil
}
