//go:build !nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"sync"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// Provider defines the interface for obtaining SPIFFE credentials.
// Implementations can connect to a local sidecar agent or an external SPIRE deployment.
type Provider interface {
	// GetX509Source returns an X509Source for obtaining SVIDs and trust bundles.
	GetX509Source(ctx context.Context) (*workloadapi.X509Source, error)

	// GetTLSConfig returns a TLS config for mTLS server connections.
	GetTLSConfig(ctx context.Context, authorizer tlsconfig.Authorizer) (*tls.Config, error)

	// GetTrustDomain returns the configured trust domain.
	GetTrustDomain() spiffeid.TrustDomain

	// Close releases any resources held by the provider.
	Close() error
}

// Config holds SPIFFE configuration loaded from environment variables.
type Config struct {
	Enabled        bool
	TrustDomain    string
	ProviderType   string
	EndpointSocket string
}

// LoadConfig loads SPIFFE configuration from environment variables.
func LoadConfig() *Config {
	enabled := os.Getenv("SPIFFE_ENABLED") == "true"
	trustDomain := os.Getenv("SPIFFE_TRUST_DOMAIN")
	if trustDomain == "" {
		trustDomain = "harbor-satellite.local"
	}

	providerType := os.Getenv("SPIFFE_PROVIDER")
	if providerType == "" {
		providerType = "sidecar"
	}

	endpointSocket := os.Getenv("SPIFFE_ENDPOINT_SOCKET")
	if endpointSocket == "" {
		endpointSocket = "unix:///run/spire/sockets/agent.sock"
	}

	return &Config{
		Enabled:        enabled,
		TrustDomain:    trustDomain,
		ProviderType:   providerType,
		EndpointSocket: endpointSocket,
	}
}

// SidecarProvider connects to a local SPIRE agent via the Workload API socket.
type SidecarProvider struct {
	config     *Config
	x509Source *workloadapi.X509Source
	td         spiffeid.TrustDomain
	once       sync.Once
	initErr    error
}

// NewSidecarProvider creates a provider that connects to a local SPIRE agent sidecar.
func NewSidecarProvider(cfg *Config) (*SidecarProvider, error) {
	td, err := spiffeid.TrustDomainFromString(cfg.TrustDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid trust domain %q: %w", cfg.TrustDomain, err)
	}

	return &SidecarProvider{
		config: cfg,
		td:     td,
	}, nil
}

// GetX509Source returns an X509Source connected to the local SPIRE agent.
// Uses sync.Once to ensure thread-safe initialization.
func (p *SidecarProvider) GetX509Source(ctx context.Context) (*workloadapi.X509Source, error) {
	p.once.Do(func() {
		source, err := workloadapi.NewX509Source(
			ctx,
			workloadapi.WithClientOptions(workloadapi.WithAddr(p.config.EndpointSocket)),
		)
		if err != nil {
			p.initErr = fmt.Errorf("create X509Source: %w", err)
			return
		}
		p.x509Source = source
	})
	return p.x509Source, p.initErr
}

// GetTLSConfig returns a TLS config for mTLS server connections using SPIFFE.
func (p *SidecarProvider) GetTLSConfig(ctx context.Context, authorizer tlsconfig.Authorizer) (*tls.Config, error) {
	source, err := p.GetX509Source(ctx)
	if err != nil {
		return nil, err
	}

	if authorizer == nil {
		authorizer = tlsconfig.AuthorizeAny()
	}

	return tlsconfig.MTLSServerConfig(source, source, authorizer), nil
}

// GetTrustDomain returns the configured trust domain.
func (p *SidecarProvider) GetTrustDomain() spiffeid.TrustDomain {
	return p.td
}

// Close releases resources held by the provider.
func (p *SidecarProvider) Close() error {
	if p.x509Source != nil {
		return p.x509Source.Close()
	}
	return nil
}

// StaticProvider uses pre-loaded certificates for environments without SPIRE agent.
// Useful for testing or gradual migration.
type StaticProvider struct {
	svid   *x509svid.SVID
	bundle *x509bundle.Bundle
	td     spiffeid.TrustDomain
}

// NewStaticProvider creates a provider with pre-loaded certificates.
func NewStaticProvider(certFile, keyFile, bundleFile, trustDomain string) (*StaticProvider, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("invalid trust domain %q: %w", trustDomain, err)
	}

	svid, err := x509svid.Load(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load SVID from %s/%s: %w", certFile, keyFile, err)
	}

	bundle, err := x509bundle.Load(td, bundleFile)
	if err != nil {
		return nil, fmt.Errorf("load bundle from %s: %w", bundleFile, err)
	}

	return &StaticProvider{
		svid:   svid,
		bundle: bundle,
		td:     td,
	}, nil
}

// GetX509Source is not supported for StaticProvider.
// Use GetTLSConfig directly instead.
func (p *StaticProvider) GetX509Source(_ context.Context) (*workloadapi.X509Source, error) {
	return nil, fmt.Errorf("X509Source not available for static provider; use GetTLSConfig")
}

// GetTLSConfig returns a TLS config using the static SVID and bundle.
func (p *StaticProvider) GetTLSConfig(_ context.Context, authorizer tlsconfig.Authorizer) (*tls.Config, error) {
	if authorizer == nil {
		authorizer = tlsconfig.AuthorizeAny()
	}

	return tlsconfig.MTLSServerConfig(p.svid, p.bundle, authorizer), nil
}

// GetTrustDomain returns the configured trust domain.
func (p *StaticProvider) GetTrustDomain() spiffeid.TrustDomain {
	return p.td
}

// Close releases resources (no-op for static provider).
func (p *StaticProvider) Close() error {
	return nil
}

// NewProvider creates a Provider based on the configuration.
func NewProvider(cfg *Config) (Provider, error) {
	switch cfg.ProviderType {
	case "sidecar":
		return NewSidecarProvider(cfg)
	case "static":
		certFile := os.Getenv("SPIFFE_CERT_FILE")
		keyFile := os.Getenv("SPIFFE_KEY_FILE")
		bundleFile := os.Getenv("SPIFFE_BUNDLE_FILE")
		if certFile == "" || keyFile == "" || bundleFile == "" {
			return nil, fmt.Errorf("static provider requires SPIFFE_CERT_FILE, SPIFFE_KEY_FILE, and SPIFFE_BUNDLE_FILE")
		}
		return NewStaticProvider(certFile, keyFile, bundleFile, cfg.TrustDomain)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", cfg.ProviderType)
	}
}
