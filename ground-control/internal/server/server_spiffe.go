//go:build !nospiffe

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
)

// buildSPIFFETLSConfig creates a TLS config using SPIFFE for mTLS authentication.
func buildSPIFFETLSConfig(provider spiffe.Provider, _ *spiffe.Config) (*tls.Config, error) {
	td := provider.GetTrustDomain()
	authorizer := spiffe.NewSatelliteAuthorizer(td)

	tlsConfig, err := provider.GetTLSConfig(context.Background(), authorizer.AuthorizeID())
	if err != nil {
		return nil, fmt.Errorf("build SPIFFE TLS config: %w", err)
	}

	// RequestClientCert asks for a client cert but does not require it and
	// does not trigger Go's standard x509 verification. go-spiffe uses
	// RequireAnyClientCert which also skips x509 verification, but requires
	// a cert. We downgrade to RequestClientCert so public endpoints (ping,
	// health, login) work without certs while SPIFFE's VerifyPeerCertificate
	// callback still validates certs when present.
	tlsConfig.ClientAuth = tls.RequestClientCert

	// Wrap VerifyPeerCertificate to skip verification when no client cert is
	// presented. Go's TLS calls this callback even with empty certs.
	origVerify := tlsConfig.VerifyPeerCertificate
	if origVerify != nil {
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return nil
			}
			return origVerify(rawCerts, verifiedChains)
		}
	}

	// Wrap GetConfigForClient so per-connection configs also use
	// RequestClientCert instead of RequireAnyClientCert.
	origGetConfig := tlsConfig.GetConfigForClient
	if origGetConfig != nil {
		tlsConfig.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			cfg, err := origGetConfig(hello)
			if err != nil {
				return nil, err
			}
			if cfg != nil {
				cfg.ClientAuth = tls.RequestClientCert
				innerVerify := cfg.VerifyPeerCertificate
				if innerVerify != nil {
					cfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
						if len(rawCerts) == 0 {
							return nil
						}
						return innerVerify(rawCerts, verifiedChains)
					}
				}
			}
			return cfg, nil
		}
	}

	return tlsConfig, nil
}
