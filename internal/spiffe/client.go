//go:build !nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// Client provides SPIFFE-based authentication for the Satellite.
type Client struct {
	socketPath       string
	expectedServerID spiffeid.ID
	x509Source       *workloadapi.X509Source
	mu               sync.RWMutex
	closed           bool
}

// Config holds SPIFFE client configuration.
type Config struct {
	Enabled          bool   `json:"enabled,omitempty"`
	EndpointSocket   string `json:"endpoint_socket,omitempty"`
	ExpectedServerID string `json:"expected_server_id,omitempty"`
}

// DefaultConfig returns the default SPIFFE client configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:          false,
		EndpointSocket:   "unix:///run/spire/sockets/agent.sock",
		ExpectedServerID: "",
	}
}

// NewClient creates a new SPIFFE client.
func NewClient(cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("SPIFFE is not enabled")
	}

	var serverID spiffeid.ID
	if cfg.ExpectedServerID != "" {
		var err error
		serverID, err = spiffeid.FromString(cfg.ExpectedServerID)
		if err != nil {
			return nil, fmt.Errorf("invalid expected server ID %q: %w", cfg.ExpectedServerID, err)
		}
	}

	return &Client{
		socketPath:       cfg.EndpointSocket,
		expectedServerID: serverID,
	}, nil
}

// Connect establishes a connection to the SPIRE agent and obtains an X509Source.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	if c.x509Source != nil {
		return nil
	}

	source, err := workloadapi.NewX509Source(
		ctx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(c.socketPath)),
	)
	if err != nil {
		return fmt.Errorf("create X509Source: %w", err)
	}

	c.x509Source = source
	return nil
}

// GetSVID returns the current X.509 SVID.
func (c *Client) GetSVID() (*x509svid.SVID, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("client is closed")
	}

	if c.x509Source == nil {
		return nil, fmt.Errorf("client not connected")
	}

	return c.x509Source.GetX509SVID()
}

// GetSPIFFEID returns the SPIFFE ID of this workload.
func (c *Client) GetSPIFFEID() (spiffeid.ID, error) {
	svid, err := c.GetSVID()
	if err != nil {
		return spiffeid.ID{}, err
	}
	return svid.ID, nil
}

// GetTLSConfig returns a TLS config for mTLS client connections.
func (c *Client) GetTLSConfig() (*tls.Config, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("client is closed")
	}

	if c.x509Source == nil {
		return nil, fmt.Errorf("client not connected")
	}

	var authorizer tlsconfig.Authorizer
	if !c.expectedServerID.IsZero() {
		authorizer = tlsconfig.AuthorizeID(c.expectedServerID)
	} else {
		authorizer = tlsconfig.AuthorizeAny()
	}

	return tlsconfig.MTLSClientConfig(c.x509Source, c.x509Source, authorizer), nil
}

// CreateHTTPClient creates an HTTP client configured for SPIFFE mTLS.
func (c *Client) CreateHTTPClient() (*http.Client, error) {
	tlsConfig, err := c.GetTLSConfig()
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     tlsConfig,
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
		},
		Timeout: 30 * time.Second,
	}, nil
}

// Close releases resources held by the client.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	if c.x509Source != nil {
		return c.x509Source.Close()
	}
	return nil
}

// WaitForSVID waits for an SVID to become available, with timeout.
func (c *Client) WaitForSVID(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for SVID: %w", ctx.Err())
		case <-ticker.C:
			_, err := c.GetSVID()
			if err == nil {
				return nil
			}
		}
	}
}

// ExtractSPIFFEIDFromCert extracts the SPIFFE ID from an X.509 certificate.
func ExtractSPIFFEIDFromCert(cert *x509.Certificate) (spiffeid.ID, error) {
	if len(cert.URIs) == 0 {
		return spiffeid.ID{}, fmt.Errorf("certificate has no URI SANs")
	}

	for _, uri := range cert.URIs {
		if uri.Scheme == "spiffe" {
			return spiffeid.FromURI(uri)
		}
	}

	return spiffeid.ID{}, fmt.Errorf("no SPIFFE ID found in certificate URIs")
}
