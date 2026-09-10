//go:build nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"errors"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

var ErrSPIFFENotAvailable = errors.New("SPIFFE support not compiled in (nospiffe build)")

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

// LoadConfig loads SPIFFE configuration from environment variables.
func LoadConfig() *Config {
	cfg := env.GC.SPIFFE

	return &Config{
		Enabled:        cfg.Enabled,
		TrustDomain:    cfg.TrustDomain,
		ProviderType:   cfg.Provider,
		EndpointSocket: cfg.EndpointSocket,
	}
}

// NewProvider returns a nil provider and no error if SPIFFE is disabled.
// If SPIFFE is enabled in configuration, it returns ErrSPIFFENotAvailable because SPIFFE is not supported in this build.
func NewProvider(cfg *Config) (Provider, error) {
	if cfg.Enabled {
		return nil, ErrSPIFFENotAvailable
	}
	return nil, nil
}
