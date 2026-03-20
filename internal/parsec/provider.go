//go:build parsec

package parsec

// KeyProvider implements crypto.Provider using hardware-backed PARSEC operations.
//
// Key design decisions:
//
//  1. Private keys are NEVER exported. GenerateKeyPair() returns a KeyRef (name
//     reference) as the "private key" and the actual exported public key bytes.
//     Callers that need to sign must pass a KeyRef to Sign().
//
//  2. Config sealing uses a fixed named AES key (configSealKeyName). Because the
//     key lives in hardware, it IS device-specific — a cloned device does not have
//     the same hardware key, even if it has a copy of the ciphertext.
//     This replaces the software device-fingerprint approach in AESProvider.
//
//  3. DeriveKey and Hash remain software operations. Key derivation is not the
//     same as key storage; PARSEC is used where non-exportability matters.

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"fmt"

	parsecclient "github.com/parallaxsecond/parsec-client-go/parsec"
	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
	"golang.org/x/crypto/argon2"
)

const (
	nonceSize     = 12
	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 4
)

// KeyProvider implements the satellite's crypto.Provider interface using PARSEC.
type KeyProvider struct {
	client *parsecclient.BasicClient
}

// NewKeyProvider creates a KeyProvider connected to the PARSEC daemon at socketPath.
// Call MustDetect(socketPath) before this to get a clear error if the daemon is absent.
func NewKeyProvider(socketPath string) (*KeyProvider, error) {
	cfg := parsecclient.NewClientConfig().WithSocketPath(socketPath)
	client, err := parsecclient.CreateConfiguredClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to parsec daemon at %s: %w", socketPath, err)
	}

	p := &KeyProvider{client: client}

	// Ensure both persistent keys exist on first boot; no-op on subsequent boots.
	if err := p.ensureIdentityKey(); err != nil {
		return nil, err
	}
	if err := p.ensureConfigSealKey(); err != nil {
		return nil, err
	}

	return p, nil
}

// Close releases the PARSEC client connection.
func (p *KeyProvider) Close() error {
	return p.client.Close()
}

// --- crypto.Provider implementation ---

// Encrypt seals plaintext using the hardware-resident config seal key (AES-256-GCM).
// The key parameter is ignored; the hardware key is the source of trust.
// Ciphertext format: [12-byte nonce][AEAD ciphertext+tag].
func (p *KeyProvider) Encrypt(plaintext, _ []byte) ([]byte, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	aeadAlg := algorithm.NewAead().AesGcm(algorithm.HashAlgorithmTypeSHA256).GetAead()
	ciphertext, err := p.client.PsaAeadEncrypt(configSealKeyName, aeadAlg, nonce, nil, plaintext)
	if err != nil {
		return nil, fmt.Errorf("parsec aead encrypt: %w", err)
	}

	return append(nonce, ciphertext...), nil
}

// Decrypt unseals ciphertext produced by Encrypt.
// The key parameter is ignored; the hardware key is the source of trust.
func (p *KeyProvider) Decrypt(ciphertext, _ []byte) ([]byte, error) {
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := ciphertext[:nonceSize]
	body := ciphertext[nonceSize:]

	aeadAlg := algorithm.NewAead().AesGcm(algorithm.HashAlgorithmTypeSHA256).GetAead()
	plaintext, err := p.client.PsaAeadDecrypt(configSealKeyName, aeadAlg, nonce, nil, body)
	if err != nil {
		return nil, fmt.Errorf("parsec aead decrypt: %w", err)
	}
	return plaintext, nil
}

// DeriveKey uses software Argon2id. Key derivation is not the same as key
// storage; there is no reason to involve hardware for this operation.
func (p *KeyProvider) DeriveKey(input, salt []byte, keyLen int) ([]byte, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	if keyLen <= 0 {
		return nil, fmt.Errorf("invalid key length %d", keyLen)
	}
	return argon2.IDKey(input, salt, argon2Time, argon2Memory, argon2Threads, uint32(keyLen)), nil
}

// Sign signs data using the hardware-resident identity key.
// key must be a KeyRef pointing to the named PARSEC key.
// The private key material never leaves the secure element.
func (p *KeyProvider) Sign(data []byte, key crypto.PrivateKey) ([]byte, error) {
	ref, ok := key.(KeyRef)
	if !ok {
		return nil, fmt.Errorf("parsec provider requires KeyRef, got %T — use GenerateKeyPair() to obtain the key reference", key)
	}

	hash := sha256.Sum256(data)
	sigAlg := algorithm.NewAsymmetricSignature().
		Ecdsa(algorithm.HashAlgorithmTypeSHA256).
		GetAsymmetricSignature()

	sig, err := p.client.PsaSignHash(ref.Name, hash[:], sigAlg)
	if err != nil {
		return nil, fmt.Errorf("parsec sign with key %q: %w", ref.Name, err)
	}
	return sig, nil
}

// Verify verifies a signature against data using the hardware-stored public key.
func (p *KeyProvider) Verify(data, signature []byte, key crypto.PublicKey) error {
	ref, ok := key.(KeyRef)
	if !ok {
		return fmt.Errorf("parsec provider requires KeyRef for Verify, got %T", key)
	}

	hash := sha256.Sum256(data)
	sigAlg := algorithm.NewAsymmetricSignature().
		Ecdsa(algorithm.HashAlgorithmTypeSHA256).
		GetAsymmetricSignature()

	if err := p.client.PsaVerifyHash(ref.Name, hash[:], signature, sigAlg); err != nil {
		return fmt.Errorf("parsec verify: %w", err)
	}
	return nil
}

// GenerateKeyPair ensures the hardware identity key exists and returns:
//   - crypto.PrivateKey: a KeyRef (name reference — the private key NEVER leaves hardware)
//   - crypto.PublicKey:  the actual exported public key (x509.PublicKey)
//
// If the key was already generated (e.g. on a previous boot), it is loaded, not regenerated.
func (p *KeyProvider) GenerateKeyPair() (crypto.PrivateKey, crypto.PublicKey, error) {
	if err := p.ensureIdentityKey(); err != nil {
		return nil, nil, err
	}

	pubDER, err := p.client.PsaExportPublicKey(identityKeyName)
	if err != nil {
		return nil, nil, fmt.Errorf("export public key from parsec: %w", err)
	}

	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parse exported public key: %w", err)
	}

	return KeyRef{Name: identityKeyName}, pub, nil
}

// NewSigner returns a crypto.Signer backed by the hardware identity key.
// Use this when you need to hand a key to standard Go TLS (e.g. tls.Certificate).
func (p *KeyProvider) NewSigner() (*Signer, error) {
	return NewSigner(p.client, identityKeyName)
}

// Hash computes a SHA-256 hash in software.
func (p *KeyProvider) Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// RandomBytes generates cryptographically secure random bytes using PARSEC.
func (p *KeyProvider) RandomBytes(n int) ([]byte, error) {
	b, err := p.client.PsaGenerateRandom(uint64(n))
	if err != nil {
		return nil, fmt.Errorf("parsec generate random: %w", err)
	}
	return b, nil
}

// --- internal key lifecycle ---

// ensureIdentityKey generates the identity signing key if it does not already exist.
// Uses ECDSA P-256 with SHA-256. The key is non-exportable (private key stays in hardware).
func (p *KeyProvider) ensureIdentityKey() error {
	// ListKeys to check if key already exists; if so, skip generation.
	keys, err := p.client.ListKeys()
	if err != nil {
		return fmt.Errorf("list parsec keys: %w", err)
	}
	for _, k := range keys {
		if k.Name == identityKeyName {
			return nil // already exists
		}
	}

	attrs := parsecclient.DefaultKeyAttribute().SigningKey()
	if err := p.client.PsaGenerateKey(identityKeyName, attrs); err != nil {
		return fmt.Errorf("generate identity key in parsec: %w", err)
	}
	return nil
}

// ensureConfigSealKey generates the AES-256-GCM config sealing key if it does not exist.
func (p *KeyProvider) ensureConfigSealKey() error {
	keys, err := p.client.ListKeys()
	if err != nil {
		return fmt.Errorf("list parsec keys: %w", err)
	}
	for _, k := range keys {
		if k.Name == configSealKeyName {
			return nil
		}
	}

	attrs := parsecclient.DefaultKeyAttribute().AeadKey()
	if err := p.client.PsaGenerateKey(configSealKeyName, attrs); err != nil {
		return fmt.Errorf("generate config seal key in parsec: %w", err)
	}
	return nil
}
