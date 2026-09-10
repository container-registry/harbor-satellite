package crypto

import (
	"crypto"
	"errors"
)

var (
	ErrDecryptionFailed  = errors.New("decryption failed")
	ErrInvalidKey        = errors.New("invalid key")
	ErrInvalidKeyLength  = errors.New("invalid key length")
	ErrInvalidInput      = errors.New("invalid input")
	ErrSignatureMismatch = errors.New("signature verification failed")
)

// Provider abstracts cryptographic operations for the satellite.
// This interface allows for testing with mocks and swapping implementations.
type Provider interface {
	// Encrypt encrypts plaintext data using the provided key.
	// Returns ciphertext or error on failure.
	Encrypt(plaintext, key []byte) ([]byte, error)

	// Decrypt decrypts ciphertext using the provided key.
	// Returns plaintext or error on failure.
	Decrypt(ciphertext, key []byte) ([]byte, error)

	// DeriveKey derives a cryptographic key from input material using a KDF.
	// salt should be random or unique per derivation.
	// keyLen specifies the desired output key length in bytes.
	DeriveKey(input, salt []byte, keyLen int) ([]byte, error)

	// Sign signs data using the provided private key.
	Sign(data []byte, key crypto.PrivateKey) ([]byte, error)

	// Verify verifies a signature against data using the public key.
	Verify(data, signature []byte, key crypto.PublicKey) error

	// GenerateKeyPair generates a new asymmetric key pair.
	GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error)

	// Hash computes a cryptographic hash of the data.
	Hash(data []byte) []byte

	// RandomBytes generates cryptographically secure random bytes.
	RandomBytes(n int) ([]byte, error)
}
