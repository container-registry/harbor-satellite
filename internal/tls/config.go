package tls

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	ErrCertNotFound      = errors.New("certificate file not found")
	ErrKeyNotFound       = errors.New("key file not found")
	ErrCertInvalid       = errors.New("certificate invalid")
	ErrCertExpired       = errors.New("certificate expired")
	ErrCertNotYetValid   = errors.New("certificate not yet valid")
	ErrCANotFound        = errors.New("CA certificate not found")
	ErrCAInvalid         = errors.New("CA certificate invalid")
	ErrCertKeyMismatch   = errors.New("certificate and key do not match")
	ErrNoCertificates    = errors.New("no certificates in file")
)

// Config holds TLS configuration options.
type Config struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
	SkipVerify bool
	MinVersion uint16
	Signer     crypto.Signer
}

// DefaultConfig returns a secure default TLS configuration.
func DefaultConfig() *Config {
	return &Config{
		MinVersion: tls.VersionTLS12,
		SkipVerify: false,
	}
}

// LoadClientTLSConfig loads TLS configuration for a client with mTLS support.
func LoadClientTLSConfig(cfg *Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: cfg.ServerName,
	}
	if cfg.SkipVerify {
		tlsConfig.InsecureSkipVerify = cfg.SkipVerify
	}

	// Load client certificate if provided (for mTLS)
	if cfg.CertFile != "" && (cfg.KeyFile != "" || cfg.Signer != nil) {
		cert, err := LoadCertificate(cfg.CertFile, cfg.KeyFile, cfg.Signer)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{*cert}
	}

	// Load CA certificate if provided
	if cfg.CAFile != "" {
		caPool, err := LoadCAPool(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs = caPool
	}

	return tlsConfig, nil
}

// LoadServerTLSConfig loads TLS configuration for a server.
func LoadServerTLSConfig(cfg *Config) (*tls.Config, error) {
	if cfg.CertFile == "" || (cfg.KeyFile == "" && cfg.Signer == nil) {
		return nil, ErrCertNotFound
	}

	cert, err := LoadCertificate(cfg.CertFile, cfg.KeyFile, cfg.Signer)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{*cert},
	}

	// Load CA for client certificate verification (mTLS)
	if cfg.CAFile != "" {
		caPool, err := LoadCAPool(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientCAs = caPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}

// LoadCertificate loads a certificate and key from files, or uses the provided hardware signer.
//
// When `signer` is non-nil (PARSEC hardware path), the leaf certificate is
// parsed (`x509.ParseCertificate`), its validity window is checked, and its
// public key is matched against `signer.Public()` using the per-key-type
// `Equal` method. Any mismatch refuses to start, since a signer that doesn't
// match the cert produces handshakes that fail with cryptic mid-connection
// errors. Do NOT use `cert.Leaf` here — it is not populated on the PARSEC
// path; parse `cert.Certificate[0]` directly.
func LoadCertificate(certFile, keyFile string, signer crypto.Signer) (*tls.Certificate, error) {
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		return nil, ErrCertNotFound
	}

	// --- HARDWARE-BACKED PATH (PARSEC / crypto.Signer) ---
	if signer != nil {
		certPEMBlock, err := os.ReadFile(filepath.Clean(certFile))
		if err != nil {
			return nil, err
		}

		var cert tls.Certificate
		for {
			var block *pem.Block
			block, certPEMBlock = pem.Decode(certPEMBlock)
			if block == nil {
				break
			}
			if block.Type == "CERTIFICATE" {
				cert.Certificate = append(cert.Certificate, block.Bytes)
			}
		}
		if len(cert.Certificate) == 0 {
			return nil, ErrNoCertificates
		}

		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("%w: parse leaf: %v", ErrCertInvalid, err)
		}
		now := time.Now()
		if now.Before(leaf.NotBefore) {
			return nil, ErrCertNotYetValid
		}
		if now.After(leaf.NotAfter) {
			return nil, ErrCertExpired
		}
		if err := signerMatchesCert(signer, leaf.PublicKey); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCertKeyMismatch, err)
		}

		cert.PrivateKey = signer
		cert.Leaf = leaf
		return &cert, nil
	}

	// --- SOFTWARE PATH (PEM-encoded private key on disk) ---
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return nil, ErrKeyNotFound
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCertInvalid, err)
	}

	return &cert, nil
}

// signerMatchesCert returns nil if the signer's public key matches certPub
// (per-key-type Equal), otherwise an error describing the mismatch.
func signerMatchesCert(signer crypto.Signer, certPub crypto.PublicKey) error {
	signerPub := signer.Public()
	switch sp := signerPub.(type) {
	case *ecdsa.PublicKey:
		cp, ok := certPub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("signer is ECDSA but cert public key is %T", certPub)
		}
		if !sp.Equal(cp) {
			return errors.New("ECDSA signer public key does not match certificate public key")
		}
	case *rsa.PublicKey:
		cp, ok := certPub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("signer is RSA but cert public key is %T", certPub)
		}
		if !sp.Equal(cp) {
			return errors.New("RSA signer public key does not match certificate public key")
		}
	case ed25519.PublicKey:
		cp, ok := certPub.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("signer is Ed25519 but cert public key is %T", certPub)
		}
		if !sp.Equal(cp) {
			return errors.New("Ed25519 signer public key does not match certificate public key")
		}
	default:
		return fmt.Errorf("unsupported signer public key type %T", signerPub)
	}
	return nil
}

// LoadCAPool loads a CA certificate pool from a file.
func LoadCAPool(caFile string) (*x509.CertPool, error) {
	if _, err := os.Stat(caFile); os.IsNotExist(err) {
		return nil, ErrCANotFound
	}

	caData, err := os.ReadFile(filepath.Clean(caFile))
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caData) {
		return nil, ErrCAInvalid
	}

	return caPool, nil
}

// ValidateCertificate validates a certificate file.
func ValidateCertificate(certFile string) error {
	certData, err := os.ReadFile(filepath.Clean(certFile))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrCertNotFound
		}
		return err
	}

	certs, err := ParseCertificates(certData)
	if err != nil {
		return err
	}

	if len(certs) == 0 {
		return ErrNoCertificates
	}

	// Validate the first certificate (leaf)
	cert := certs[0]
	now := time.Now()

	if now.Before(cert.NotBefore) {
		return ErrCertNotYetValid
	}

	if now.After(cert.NotAfter) {
		return ErrCertExpired
	}

	return nil
}

// ParseCertificates parses PEM-encoded certificates.
func ParseCertificates(pemData []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate

	for len(pemData) > 0 {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCertInvalid, err)
		}

		certs = append(certs, cert)
	}

	return certs, nil
}

// CertificateInfo returns information about a certificate.
type CertificateInfo struct {
	Subject    string
	Issuer     string
	NotBefore  time.Time
	NotAfter   time.Time
	IsExpired  bool
	DaysToExpiry int
	DNSNames   []string
}

// GetCertificateInfo extracts information from a certificate file.
func GetCertificateInfo(certFile string) (*CertificateInfo, error) {
	certData, err := os.ReadFile(filepath.Clean(certFile))
	if err != nil {
		return nil, err
	}

	certs, err := ParseCertificates(certData)
	if err != nil {
		return nil, err
	}

	if len(certs) == 0 {
		return nil, ErrNoCertificates
	}

	cert := certs[0]
	now := time.Now()
	daysToExpiry := int(cert.NotAfter.Sub(now).Hours() / 24)

	return &CertificateInfo{
		Subject:      cert.Subject.String(),
		Issuer:       cert.Issuer.String(),
		NotBefore:    cert.NotBefore,
		NotAfter:     cert.NotAfter,
		IsExpired:    now.After(cert.NotAfter),
		DaysToExpiry: daysToExpiry,
		DNSNames:     cert.DNSNames,
	}, nil
}
