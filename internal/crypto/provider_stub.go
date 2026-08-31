//go:build nospiffe

package crypto

import "crypto"

// EncryptionAvailable reports whether this build can actually encrypt. It is
// false here: the nospiffe build ships no cryptographic implementation.
// Callers that are about to persist secrets should check it and refuse rather
// than write data that only looks protected.
const EncryptionAvailable = false

// UnavailableProvider is the Provider compiled into the nospiffe build. It
// performs no cryptography at all and every operation fails with
// ErrCryptoUnavailable.
//
// It deliberately fails closed. An earlier version of this stub returned the
// plaintext unchanged from Encrypt, reported every signature as valid from
// Verify, and filled RandomBytes with zeros. Config written by such a build
// still carried the version header that marks a file as encrypted, and
// IsEncrypted still recognised it, so Harbor robot credentials sat on the edge
// device in plainly readable form with nothing to reveal that encryption had
// not happened. Returning an error is noisy, but a build that cannot encrypt
// must say so rather than pretend.
type UnavailableProvider struct{}

// NewUnavailableProvider returns the non-functional provider used by the
// nospiffe build.
func NewUnavailableProvider() *UnavailableProvider {
	return &UnavailableProvider{}
}

// NewDefaultProvider returns the Provider for this build. In the nospiffe
// build that is UnavailableProvider, which refuses every operation; the
// default build returns the real AES-GCM provider instead.
func NewDefaultProvider() *UnavailableProvider {
	return NewUnavailableProvider()
}

func (p *UnavailableProvider) Encrypt(_, _ []byte) ([]byte, error) {
	return nil, ErrCryptoUnavailable
}

func (p *UnavailableProvider) Decrypt(_, _ []byte) ([]byte, error) {
	return nil, ErrCryptoUnavailable
}

func (p *UnavailableProvider) DeriveKey(_, _ []byte, _ int) ([]byte, error) {
	return nil, ErrCryptoUnavailable
}

func (p *UnavailableProvider) Sign(_ []byte, _ crypto.PrivateKey) ([]byte, error) {
	return nil, ErrCryptoUnavailable
}

func (p *UnavailableProvider) Verify(_, _ []byte, _ crypto.PublicKey) error {
	return ErrCryptoUnavailable
}

func (p *UnavailableProvider) GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error) {
	return nil, nil, ErrCryptoUnavailable
}

// Hash returns nil because this build computes no hashes. The Provider
// interface gives Hash no error to return, so callers must treat an empty
// result as a failure; the previous stub returned the input unchanged, which
// is an identity function wearing the name of a hash.
func (p *UnavailableProvider) Hash(_ []byte) []byte {
	return nil
}

func (p *UnavailableProvider) RandomBytes(_ int) ([]byte, error) {
	return nil, ErrCryptoUnavailable
}
