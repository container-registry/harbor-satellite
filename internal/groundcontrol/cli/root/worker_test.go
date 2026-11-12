//go:build !nospiffe

package root

import (
	"path/filepath"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

func TestWorkerSPIREClient_ConnectsToTheConfiguredSocket(t *testing.T) {
	t.Parallel()
	client := workerSPIREClient(env.SPIRE{
		ServerSocket: filepath.Join(t.TempDir(), "spire.sock"),
		TrustDomain:  "harbor-satellite.local",
	})
	require.NotNil(t, client, "a configured SPIRE_SERVER_SOCKET gives the worker a SPIRE client")
	require.Equal(t, "harbor-satellite.local", client.TrustDomainName())
	require.NoError(t, client.Close())
}

func TestWorkerSPIREClient_NilWithoutSocket(t *testing.T) {
	t.Parallel()
	require.Nil(t, workerSPIREClient(env.SPIRE{TrustDomain: "harbor-satellite.local"}))
}
