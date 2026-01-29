//go:build nospiffe

package spiffe

import (
	"context"
	"fmt"
)

// EmbeddedSpireConfig holds configuration for the embedded SPIRE server.
type EmbeddedSpireConfig struct {
	Enabled     bool
	DataDir     string
	TrustDomain string
	BindAddress string
	BindPort    int
}

// EmbeddedSpireServer stub for nospiffe builds.
type EmbeddedSpireServer struct{}

// NewEmbeddedSpireServer returns a stub in nospiffe builds.
func NewEmbeddedSpireServer(_ *EmbeddedSpireConfig) *EmbeddedSpireServer {
	return &EmbeddedSpireServer{}
}

// Start returns an error in nospiffe builds.
func (s *EmbeddedSpireServer) Start(_ context.Context) error {
	return fmt.Errorf("SPIFFE support not compiled in (nospiffe build)")
}

// Stop is a no-op in nospiffe builds.
func (s *EmbeddedSpireServer) Stop() error {
	return nil
}

// GetClient returns nil in nospiffe builds.
func (s *EmbeddedSpireServer) GetClient() *ServerClient {
	return nil
}

// GetTrustDomain returns empty string in nospiffe builds.
func (s *EmbeddedSpireServer) GetTrustDomain() string {
	return ""
}

// GetSocketPath returns empty string in nospiffe builds.
func (s *EmbeddedSpireServer) GetSocketPath() string {
	return ""
}
