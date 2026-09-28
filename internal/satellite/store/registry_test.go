package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

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

func TestRegistryStoreReusesAuthClientAndRefreshesCredentials(t *testing.T) {
	var expectedPassword atomic.Value
	expectedPassword.Store("first")
	registryHandler := registry.New()
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, password, ok := request.BasicAuth()
		expected, valid := expectedPassword.Load().(string)
		if !ok || !valid || password != expected {
			response.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		registryHandler.ServeHTTP(response, request)
	}))
	t.Cleanup(upstream.Close)

	options := RegistryOptions{Endpoint: strings.TrimPrefix(upstream.URL, "http://"), Username: "robot", Password: "first", PlainHTTP: true}
	storage, err := NewRegistryStoreWithOptions(func() (RegistryOptions, error) { return options, nil })
	require.NoError(t, err)
	first, err := storage.repository(Artifact{Name: "team/app"})
	require.NoError(t, err)
	second, err := storage.repository(Artifact{Name: "team/app"})
	require.NoError(t, err)
	require.Same(t, first.Client, second.Client)
	_, err = storage.Pull(context.Background(), Artifact{Name: "team/app", Tag: "missing"}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)

	options.Password = "second"
	expectedPassword.Store("second")
	third, err := storage.repository(Artifact{Name: "team/app"})
	require.NoError(t, err)
	require.NotSame(t, first.Client, third.Client)
	_, err = storage.Pull(context.Background(), Artifact{Name: "team/app", Tag: "missing"}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
}
