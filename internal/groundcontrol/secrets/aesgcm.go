// Package secrets seals secrets Ground Control stores, such as successor
// robot secrets (plan item 8.4). It uses the stdlib AES-GCM rather than
// internal/shared/crypto, which is a no-op under the nospiffe tag.
package secrets

import "github.com/container-registry/harbor-satellite/internal/shared/notimpl"

// ParseKey decodes DATA_ENCRYPTION_KEY: base64 of exactly 32 bytes.
func ParseKey(encoded string) ([]byte, error) {
	return nil, notimpl.Err("8.4 parse data encryption key")
}

// AESGCM seals as version byte | nonce | sealed.
type AESGCM struct{}

func NewAESGCM(key []byte) (*AESGCM, error) {
	return nil, notimpl.Err("8.4 AES-GCM sealer")
}

func (a *AESGCM) Seal(plaintext []byte) ([]byte, error) {
	return nil, notimpl.Err("8.4 seal")
}

func (a *AESGCM) Open(ciphertext []byte) ([]byte, error) {
	return nil, notimpl.Err("8.4 open")
}
