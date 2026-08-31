package store

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

func TestRegistryStorePullFetchAndReplicate(t *testing.T) {
	source, manifestPayload, manifestDesc, _, _ := testArtifact(t, "team/app", "latest")
	destinationServer := httptest.NewServer(registry.New())
	t.Cleanup(destinationServer.Close)
	destination, err := NewRegistryStore(RegistryOptions{
		Endpoint: strings.TrimPrefix(destinationServer.URL, "http://"), PlainHTTP: true,
	})
	require.NoError(t, err)
	artifact := Artifact{Name: "team/app", Tag: "latest"}
	_, err = destination.Pull(context.Background(), artifact, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	require.NoError(t, destination.Replicate(context.Background(), source, []Artifact{artifact}))
	descriptor, err := destination.Pull(context.Background(), artifact, PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, manifestDesc, descriptor)
	reader, err := destination.Fetch(context.Background(), artifact, descriptor)
	require.NoError(t, err)
	payload, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, manifestPayload, payload)
}
