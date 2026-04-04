//go:build parsec

package parsec

// Signer implements crypto.Signer backed by a hardware-resident PARSEC key.
//
// This is the solution to the Go TLS incompatibility raised in issue #327:
// standard tls.LoadX509KeyPair requires an exportable private key, but PARSEC
// keys are non-exportable by design. By implementing crypto.Signer, we can
// construct a tls.Certificate with PrivateKey set to *Signer, and standard
// Go TLS will call Sign() for all private-key operations — the key material
// never leaves the secure element.
//
// Usage:
//
//	signer, err := NewSigner(client, identityKeyName)
//	cert := tls.Certificate{
//	    Certificate: [][]byte{derCert},
//	    PrivateKey:  signer,
//	}

import (
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"io"

	parsecclient "github.com/parallaxsecond/parsec-client-go/parsec"
	"github.com/parallaxsecond/parsec-client-go/parsec/algorithm"
)

// Signer implements crypto.Signer using a named, hardware-resident PARSEC key.
// The private key material never leaves the secure element.
type Signer struct {
	client  *parsecclient.BasicClient
	keyName string
	pub     crypto.PublicKey
	sigAlg  *algorithm.AsymmetricSignatureAlgorithm
}

// KeyRef is a reference to a hardware-resident PARSEC key by name.
// It satisfies crypto.PrivateKey (which is `any`) so it can be passed to
// crypto.Provider.Sign without changing the existing interface signature.
type KeyRef struct {
	Name string
}

// NewSigner creates a Signer for the given named key.
// It exports the public key from PARSEC to satisfy crypto.Signer.Public().
// The private key material never leaves the hardware.
func NewSigner(client *parsecclient.BasicClient, keyName string) (*Signer, error) {
	pubDER, err := client.PsaExportPublicKey(keyName)
	if err != nil {
		return nil, fmt.Errorf("export public key %q from parsec: %w", keyName, err)
	}

	pub, err := x509.ParsePKIXPublicKey(pubDER)
	if err != nil {
		return nil, fmt.Errorf("parse exported public key: %w", err)
	}

	// ECDSA P-256 with SHA-256. This must match the key attributes used in
	// KeyProvider.ensureIdentityKey(). If the key type changes, update here too.
	sigAlg := algorithm.NewAsymmetricSignature().
		Ecdsa(algorithm.HashAlgorithmTypeSHA256).
		GetAsymmetricSignature()

	return &Signer{
		client:  client,
		keyName: keyName,
		pub:     pub,
		sigAlg:  sigAlg,
	}, nil
}

// Public returns the public key half. Required by crypto.Signer.
func (s *Signer) Public() crypto.PublicKey {
	return s.pub
}

// Sign delegates the signing operation to the PARSEC hardware.
// The digest is signed directly (PARSEC PsaSignHash); the rand reader is
// ignored because the hardware generates its own entropy.
// Required by crypto.Signer.
func (s *Signer) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	// If the caller passes a full message rather than a pre-hashed digest,
	// hash it first. Standard TLS always passes a pre-hashed digest.
	if opts != nil && opts.HashFunc() == crypto.Hash(0) {
		h := sha256.Sum256(digest)
		digest = h[:]
	}

	sig, err := s.client.PsaSignHash(s.keyName, digest, s.sigAlg)
	if err != nil {
		return nil, fmt.Errorf("parsec sign with key %q: %w", s.keyName, err)
	}
	return sig, nil
}
