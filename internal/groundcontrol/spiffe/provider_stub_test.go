//go:build nospiffe

package spiffe

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/stretchr/testify/require"
)

func TestProviderStub_LoadConfig(t *testing.T) {
	// Keep track of the original configuration
	previous := env.GC
	defer func() {
		env.GC = previous
	}()

	t.Setenv("SPIFFE_SERVER_ENABLED", "true")
	t.Setenv("SPIFFE_TRUST_DOMAIN", "example.org")
	t.Setenv("SPIFFE_PROVIDER", "sidecar")
	t.Setenv("SPIFFE_ENDPOINT_SOCKET", "unix:///tmp/spire-agent/public/api.sock")

	err := env.LoadGC()
	require.NoError(t, err)

	cfg := LoadConfig()
	require.True(t, cfg.Enabled)
	require.Equal(t, "example.org", cfg.TrustDomain)
	require.Equal(t, "sidecar", cfg.ProviderType)
	require.Equal(t, "unix:///tmp/spire-agent/public/api.sock", cfg.EndpointSocket)

	// Verify NewProvider returns ErrSPIFFENotAvailable when Enabled is true
	provider, err := NewProvider(cfg)
	require.ErrorIs(t, err, ErrSPIFFENotAvailable)
	require.Nil(t, provider)

	// Verify NewProvider returns nil when Enabled is false
	t.Setenv("SPIFFE_SERVER_ENABLED", "false")
	err = env.LoadGC()
	require.NoError(t, err)

	cfg = LoadConfig()
	provider, err = NewProvider(cfg)
	require.NoError(t, err)
	require.Nil(t, provider)
}
