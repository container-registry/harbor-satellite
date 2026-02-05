//go:build !nospiffe

package spiffe

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// Source wraps a SPIFFE X509Source with additional functionality
// for Ground Control's authentication needs.
type Source struct {
	provider   Provider
	x509Source *workloadapi.X509Source
	mu         sync.RWMutex
	closed     bool
}

// NewSource creates a new SPIFFE Source using the given provider.
func NewSource(ctx context.Context, provider Provider) (*Source, error) {
	source, err := provider.GetX509Source(ctx)
	if err != nil {
		return nil, fmt.Errorf("get X509Source from provider: %w", err)
	}

	return &Source{
		provider:   provider,
		x509Source: source,
	}, nil
}

// GetSVID returns the current X.509 SVID for this workload.
func (s *Source) GetSVID() (*x509svid.SVID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("source is closed")
	}

	svid, err := s.x509Source.GetX509SVID()
	if err != nil {
		return nil, fmt.Errorf("get X509SVID: %w", err)
	}

	return svid, nil
}

// GetSPIFFEID returns the SPIFFE ID of this workload.
func (s *Source) GetSPIFFEID() (spiffeid.ID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return spiffeid.ID{}, fmt.Errorf("source is closed")
	}

	svid, err := s.x509Source.GetX509SVID()
	if err != nil {
		return spiffeid.ID{}, fmt.Errorf("get X509SVID: %w", err)
	}

	return svid.ID, nil
}

// GetTrustDomain returns the trust domain of this source.
func (s *Source) GetTrustDomain() spiffeid.TrustDomain {
	return s.provider.GetTrustDomain()
}

// ServerTLSConfig returns a TLS config suitable for a server that requires
// mTLS with SPIFFE authentication.
func (s *Source) ServerTLSConfig(authorizer tlsconfig.Authorizer) (*tls.Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("source is closed")
	}

	if authorizer == nil {
		authorizer = tlsconfig.AuthorizeAny()
	}

	return tlsconfig.MTLSServerConfig(s.x509Source, s.x509Source, authorizer), nil
}

// ClientTLSConfig returns a TLS config suitable for a client connecting
// to a SPIFFE-authenticated server.
func (s *Source) ClientTLSConfig(authorizer tlsconfig.Authorizer) (*tls.Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, fmt.Errorf("source is closed")
	}

	if authorizer == nil {
		authorizer = tlsconfig.AuthorizeAny()
	}

	return tlsconfig.MTLSClientConfig(s.x509Source, s.x509Source, authorizer), nil
}

// Close releases all resources held by this source.
func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	return s.provider.Close()
}

// WatchForRotation starts watching for SVID rotations and calls the callback
// whenever the SVID is renewed. This is useful for logging or triggering
// reconnections.
func (s *Source) WatchForRotation(ctx context.Context, callback func(*x509svid.SVID)) {
	go func() {
		var lastID string
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				svid, err := s.GetSVID()
				if err != nil {
					log.Printf("SVID watch: error getting SVID: %v", err)
					continue
				}

				currentID := svid.ID.String()
				if lastID != "" && lastID != currentID {
					log.Printf("SVID rotated: %s -> %s", lastID, currentID)
					if callback != nil {
						callback(svid)
					}
				}
				lastID = currentID
			}
		}
	}()
}
