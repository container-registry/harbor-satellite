//go:build nospiffe

package server

import (
	"crypto/tls"
	"errors"

	"github.com/container-registry/harbor-satellite/ground-control/internal/spiffe"
)

// buildSPIFFETLSConfig returns an error when SPIFFE is not available.
func buildSPIFFETLSConfig(_ spiffe.Provider, _ *spiffe.Config) (*tls.Config, error) {
	return nil, errors.New("SPIFFE not available in this build")
}
