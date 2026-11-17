// Package secrets seals secrets Ground Control stores, such as successor
// robot secrets (plan item 8.4). It uses the stdlib AES-GCM rather than
// internal/shared/crypto, which is a no-op under the nospiffe tag.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	keySize       = 32
	formatVersion = 1
)

// ParseKey decodes DATA_ENCRYPTION_KEY: base64 of exactly 32 bytes.
func ParseKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("DATA_ENCRYPTION_KEY is not base64: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("DATA_ENCRYPTION_KEY decodes to %d bytes, want %d", len(key), keySize)
	}
	return key, nil
}

// AESGCM seals as version byte | nonce | sealed.
type AESGCM struct{ aead cipher.AEAD }

func NewAESGCM(key []byte) (*AESGCM, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("AES-GCM key of %d bytes, want %d", len(key), keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

func (a *AESGCM) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("seal: nonce: %w", err)
	}
	out := append([]byte{formatVersion}, nonce...)
	return a.aead.Seal(out, nonce, plaintext, []byte{formatVersion}), nil
}

func (a *AESGCM) Open(ciphertext []byte) ([]byte, error) {
	n := a.aead.NonceSize()
	if len(ciphertext) < 1+n {
		return nil, errors.New("open: sealed secret too short")
	}
	if ciphertext[0] != formatVersion {
		return nil, fmt.Errorf("open: sealed secret version %d, want %d", ciphertext[0], formatVersion)
	}
	plain, err := a.aead.Open(nil, ciphertext[1:1+n], ciphertext[1+n:], ciphertext[:1])
	if err != nil {
		return nil, fmt.Errorf("open sealed secret: %w", err)
	}
	return plain, nil
}
