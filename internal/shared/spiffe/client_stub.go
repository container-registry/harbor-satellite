//go:build nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

var ErrSPIFFENotAvailable = errors.New("SPIFFE not available in this build")

type Client struct{}

type Config struct {
	Enabled          bool   `json:"enabled,omitempty"`
	EndpointSocket   string `json:"endpoint_socket,omitempty"`
	ExpectedServerID string `json:"expected_server_id,omitempty"`
}

func DefaultConfig() Config {
	return Config{Enabled: false}
}

func NewClient(_ Config) (*Client, error) {
	return nil, ErrSPIFFENotAvailable
}

func (c *Client) Connect(_ context.Context) error {
	return ErrSPIFFENotAvailable
}

func (c *Client) GetSVID() (*x509svid.SVID, error) {
	return nil, ErrSPIFFENotAvailable
}

func (c *Client) GetSPIFFEID() (spiffeid.ID, error) {
	return spiffeid.ID{}, ErrSPIFFENotAvailable
}

func (c *Client) GetTLSConfig() (*tls.Config, error) {
	return nil, ErrSPIFFENotAvailable
}

func (c *Client) CreateHTTPClient() (*http.Client, error) {
	return nil, ErrSPIFFENotAvailable
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) WaitForSVID(_ context.Context, _ time.Duration) error {
	return ErrSPIFFENotAvailable
}

func ExtractSPIFFEIDFromCert(_ *x509.Certificate) (spiffeid.ID, error) {
	return spiffeid.ID{}, ErrSPIFFENotAvailable
}
