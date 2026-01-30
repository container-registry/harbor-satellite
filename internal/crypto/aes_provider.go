//go:build !nospiffe

package crypto

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math"

	"golang.org/x/crypto/argon2"
)

const (
	// AES-256-GCM key size
	aesKeySize = 32
	// GCM nonce size
	nonceSize = 12
	// Argon2 parameters (OWASP recommended)
	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 4
)

// AESProvider implements Provider using AES-GCM for encryption
// and Argon2id for key derivation.
type AESProvider struct{}

// NewAESProvider creates a new AESProvider instance.
func NewAESProvider() *AESProvider {
	return &AESProvider{}
}

func (p *AESProvider) Encrypt(plaintext, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}

	derivedKey := p.ensureKeySize(key)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func (p *AESProvider) Decrypt(ciphertext, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}

	if len(ciphertext) < nonceSize {
		return nil, ErrDecryptionFailed
	}

	derivedKey := p.ensureKeySize(key)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := ciphertext[:nonceSize]
	ciphertext = ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

func (p *AESProvider) DeriveKey(input, salt []byte, keyLen int) ([]byte, error) {
	if len(input) == 0 {
		return nil, ErrInvalidInput
	}
	if keyLen <= 0 || keyLen > math.MaxUint32 {
		return nil, ErrInvalidKeyLength
	}

	key := argon2.IDKey(input, salt, argon2Time, argon2Memory, argon2Threads, uint32(keyLen))
	return key, nil
}

func (p *AESProvider) Sign(data []byte, key crypto.PrivateKey) ([]byte, error) {
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected *ecdsa.PrivateKey, got %T", key)
	}

	hash := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, ecdsaKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("sign data: %w", err)
	}

	return sig, nil
}

func (p *AESProvider) Verify(data, signature []byte, key crypto.PublicKey) error {
	ecdsaKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("expected *ecdsa.PublicKey, got %T", key)
	}

	hash := sha256.Sum256(data)
	if !ecdsa.VerifyASN1(ecdsaKey, hash[:], signature) {
		return ErrSignatureMismatch
	}

	return nil
}

func (p *AESProvider) GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key pair: %w", err)
	}

	return privateKey, &privateKey.PublicKey, nil
}

func (p *AESProvider) Hash(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func (p *AESProvider) RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return b, nil
}

func (p *AESProvider) ensureKeySize(key []byte) []byte {
	if len(key) == aesKeySize {
		return key
	}
	hash := sha256.Sum256(key)
	return hash[:]
}
