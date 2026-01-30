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

	// Allow public endpoints (ping, health, login) without client certs.
	// SPIFFE auth is enforced at the middleware level for protected routes.
	tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven

	// Wrap VerifyPeerCertificate to skip SPIFFE verification when no client
	// cert is presented. Go's TLS calls this callback even with empty certs
	// when ClientAuth is VerifyClientCertIfGiven, but the go-spiffe callback
	// rejects empty certificate chains.
	origVerify := tlsConfig.VerifyPeerCertificate
	if origVerify != nil {
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return nil
			}
			return origVerify(rawCerts, verifiedChains)
		}
	}

	return tlsConfig, nil
}
