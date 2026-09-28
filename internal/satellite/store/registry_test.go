package store

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

func TestDynamicRegistryStoreResolvesOptionsAfterBootstrap(t *testing.T) {
	options := RegistryOptions{}
	calls := 0
	dynamic, err := NewDynamicRegistryStore(func() (RegistryOptions, error) {
		calls++
		return options, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)

	options.Endpoint = "harbor.example.com"
	repository, err := dynamic.repository(Artifact{Name: "team/app", Tag: "latest"})
	require.NoError(t, err)
	require.NotNil(t, repository)
	require.Equal(t, "harbor.example.com/team/app", repository.Reference.String())
	require.Equal(t, 2, calls)
}

func TestDynamicRegistryStorePropagatesProviderError(t *testing.T) {
	want := errors.New("credentials unavailable")
	_, err := NewDynamicRegistryStore(func() (RegistryOptions, error) {
		return RegistryOptions{}, want
	})
	require.ErrorIs(t, err, want)
}

func TestRegistryStorePullFetchAndReplicate(t *testing.T) {
	source, manifestPayload, manifestDesc, _, _ := testArtifact(t)
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

func TestRegistryStoreMissingSourceAndDelete(t *testing.T) {
	source, _, _, _, _ := testArtifact(t)
	destinationServer := httptest.NewServer(registry.New())
	t.Cleanup(destinationServer.Close)
	destination, err := NewRegistryStore(RegistryOptions{
		Endpoint: strings.TrimPrefix(destinationServer.URL, "http://"), PlainHTTP: true,
	})
	require.NoError(t, err)
	missing := Artifact{Name: "team/app", Tag: "missing"}
	require.Error(t, destination.Replicate(context.Background(), source, []Artifact{missing}))
	require.NoError(t, destination.Delete(context.Background(), []Artifact{missing}))
	artifact := Artifact{Name: "team/app", Tag: "latest"}
	require.NoError(t, destination.Replicate(context.Background(), source, []Artifact{artifact}))
	require.NoError(t, destination.Delete(context.Background(), []Artifact{artifact}))
	require.NoError(t, destination.Delete(context.Background(), []Artifact{missing}))
}

func TestRegistryStoreOperationsHonorCancellation(t *testing.T) {
	source, _, _, _, _ := testArtifact(t)
	destinationServer := httptest.NewServer(registry.New())
	t.Cleanup(destinationServer.Close)
	destination, err := NewRegistryStore(RegistryOptions{
		Endpoint: strings.TrimPrefix(destinationServer.URL, "http://"), PlainHTTP: true,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	artifact := Artifact{Name: "team/app", Tag: "latest"}
	require.ErrorIs(t, destination.Replicate(ctx, source, []Artifact{artifact}), context.Canceled)
	require.ErrorIs(t, destination.Delete(ctx, []Artifact{artifact}), context.Canceled)
}
