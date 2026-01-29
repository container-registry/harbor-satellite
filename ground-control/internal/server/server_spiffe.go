//go:build !nospiffe

package server

import (
	"context"
	"crypto/tls"
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

	return tlsConfig, nil
}
