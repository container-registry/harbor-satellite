//go:build nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"errors"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

var ErrSPIFFENotAvailable = errors.New("SPIFFE not available in this build")

// Provider defines the interface for obtaining SPIFFE credentials.
type Provider interface {
	GetX509Source(ctx context.Context) (*workloadapi.X509Source, error)
	GetTLSConfig(ctx context.Context, authorizer tlsconfig.Authorizer) (*tls.Config, error)
	GetTrustDomain() spiffeid.TrustDomain
	Close() error
}

// Config holds SPIFFE configuration.
type Config struct {
	Enabled        bool
	TrustDomain    string
	ProviderType   string
	EndpointSocket string
}

// LoadConfig returns config with SPIFFE disabled.
func LoadConfig() *Config {
	return &Config{Enabled: false}
}

// NewProvider returns an error since SPIFFE is not available.
func NewProvider(_ *Config) (Provider, error) {
	return nil, ErrSPIFFENotAvailable
}
