//go:build nospiffe

package spiffe

import (
	"context"
	"fmt"
	"time"
)

// ServerClient stub for nospiffe builds.
type ServerClient struct{}

// NewServerClient returns an error in nospiffe builds.
func NewServerClient(_, _ string) (*ServerClient, error) {
	return nil, fmt.Errorf("SPIFFE support not compiled in (nospiffe build)")
}

// CreateJoinToken is not available in nospiffe builds.
func (c *ServerClient) CreateJoinToken(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("SPIFFE support not compiled in (nospiffe build)")
}

// CreateWorkloadEntry is not available in nospiffe builds.
func (c *ServerClient) CreateWorkloadEntry(_ context.Context, _, _ string, _ []string) error {
	return fmt.Errorf("SPIFFE support not compiled in (nospiffe build)")
}

// Close is a no-op in nospiffe builds.
func (c *ServerClient) Close() error {
	return nil
}
