//go:build !parsec

package parsec

import "crypto"

// KeyProvider is a no-op stub in non-parsec builds.
// All methods return ErrParsecNotAvailable.
type KeyProvider struct{}

// NewKeyProvider always fails in non-parsec builds.
func NewKeyProvider(_ string) (*KeyProvider, error) {
	return nil, ErrParsecNotAvailable
}

func (p *KeyProvider) Close() error { return nil }

func (p *KeyProvider) Encrypt(_, _ []byte) ([]byte, error) {
	return nil, ErrParsecNotAvailable
}

func (p *KeyProvider) Decrypt(_, _ []byte) ([]byte, error) {
	return nil, ErrParsecNotAvailable
}

func (p *KeyProvider) DeriveKey(_, _ []byte, _ int) ([]byte, error) {
	return nil, ErrParsecNotAvailable
}

func (p *KeyProvider) Sign(_ []byte, _ crypto.PrivateKey) ([]byte, error) {
	return nil, ErrParsecNotAvailable
}

func (p *KeyProvider) Verify(_, _ []byte, _ crypto.PublicKey) error {
	return ErrParsecNotAvailable
}

func (p *KeyProvider) GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error) {
	return nil, nil, ErrParsecNotAvailable
}

func (p *KeyProvider) NewSigner() (*Signer, error) {
	return nil, ErrParsecNotAvailable
}

func (p *KeyProvider) Hash(_ []byte) []byte { return nil }

func (p *KeyProvider) RandomBytes(_ int) ([]byte, error) {
	return nil, ErrParsecNotAvailable
}

// Signer is an empty stub type in non-parsec builds.
// It exists so code that references parsec.Signer compiles regardless of build tag.
type Signer struct{}
