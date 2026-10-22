package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasremote "oras.land/oras-go/v2/registry/remote"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/process/image"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
)

func TestReciprocalReplicaCopiesFinishWithinPeerTimeout(t *testing.T) {
	alpha, _, alphaDesc := seedLayout(t, "team/alpha", []byte("alpha-layer"))
	beta, _, betaDesc := seedLayout(t, "team/beta", []byte("beta-layer"))
	// Hold both manifest reads until each replica has been asked. Otherwise a
	// tiny image can finish on one side before the other side starts, and the
	// test never overlaps the two copies.
	var entered atomic.Int32
	release := make(chan struct{})
	barrier := func(next http.Handler) http.Handler {
		var once sync.Once
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if strings.Contains(request.URL.Path, "/manifests/") {
				once.Do(func() {
					if entered.Add(1) == 2 {
						close(release)
					}
				})
				select {
				case <-release:
				case <-request.Context().Done():
					return
				}
			}
			next.ServeHTTP(writer, request)
		})
	}
	alphaPeer := registryFromServer(t, replicaServer(t, alpha, barrier))
	betaPeer := registryFromServer(t, replicaServer(t, beta, barrier))

	const peerTimeout = 5 * time.Second
	opts := store.PeerFillOptions{Timeout: peerTimeout}
	fillingAlpha, err := store.NewPeerFillingStore(alpha, func() []store.Store { return []store.Store{betaPeer} }, opts)
	require.NoError(t, err)
	fillingBeta, err := store.NewPeerFillingStore(beta, func() []store.Store { return []store.Store{alphaPeer} }, opts)
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		errs <- fillingAlpha.Replicate(context.Background(), nil, []store.Artifact{{Name: "team/beta", Tag: "latest"}})
	}()
	go func() {
		<-start
		errs <- fillingBeta.Replicate(context.Background(), nil, []store.Artifact{{Name: "team/alpha", Tag: "latest"}})
	}()
	close(start)
	for range 2 {
		require.NoError(t, <-errs)
	}

	gotBeta, err := alpha.Pull(context.Background(), store.Artifact{Name: "team/beta", Tag: "latest"}, store.PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, betaDesc.Digest, gotBeta.Digest)
	gotAlpha, err := beta.Pull(context.Background(), store.Artifact{Name: "team/alpha", Tag: "latest"}, store.PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, alphaDesc.Digest, gotAlpha.Digest)
}

func TestPeerAttemptReturnsWhileLocalFetchBodyIsOpen(t *testing.T) {
	local, _, manifestDesc := seedLayout(t, "team/app", []byte("held-layer"))
	_, remote, _ := seedLayout(t, "team/app", []byte("held-layer"))
	artifact := store.Artifact{Name: "team/app", Tag: "latest"}
	response, err := local.Fetch(context.Background(), artifact, manifestDesc, store.PullResourceManifest, nil)
	require.NoError(t, err)

	filling, err := store.NewPeerFillingStore(local, func() []store.Store { return []store.Store{remote} }, store.PeerFillOptions{Timeout: time.Second})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- filling.Replicate(context.Background(), nil, []store.Artifact{artifact})
	}()
	select {
	case replicateErr := <-done:
		require.ErrorIs(t, replicateErr, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("peer attempt blocked while a local response body stayed open")
	}

	require.NoError(t, response.Body.Close())
	pulled := make(chan error, 1)
	go func() {
		_, pullErr := local.Pull(context.Background(), artifact, store.PullResourceManifest)
		pulled <- pullErr
	}()
	select {
	case pullErr := <-pulled:
		require.NoError(t, pullErr)
	case <-time.After(2 * time.Second):
		t.Fatal("store lock stayed held after the timed out peer attempt")
	}
}

func seedLayout(t *testing.T, name string, layer []byte) (*store.OCIStore, store.Store, ocispec.Descriptor) {
	t.Helper()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	repo, err := orasremote.NewRepository(address + "/" + name)
	require.NoError(t, err)
	repo.PlainHTTP = true
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	configPayload := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	require.NoError(t, repo.Push(context.Background(), layerDesc, bytes.NewReader(layer)))
	require.NoError(t, repo.Push(context.Background(), configDesc, bytes.NewReader(configPayload)))
	require.NoError(t, repo.PushReference(context.Background(), manifestDesc, bytes.NewReader(manifestPayload), "latest"))
	remote, err := store.NewRegistryStore(store.RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)
	local, err := store.NewOCIStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, local.Replicate(context.Background(), remote, []store.Artifact{{Name: name, Tag: "latest"}}))
	return local, remote, manifestDesc
}

func replicaServer(t *testing.T, local store.Store, wrap func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	handler, err := image.NewPull(context.Background(), proxy.ModeReplica, local, nil)
	require.NoError(t, err)
	httpHandler := proxy.New(handler).Handler()
	if wrap != nil {
		httpHandler = wrap(httpHandler)
	}
	server := httptest.NewServer(httpHandler)
	t.Cleanup(server.Close)
	return server
}

func registryFromServer(t *testing.T, server *httptest.Server) store.Store {
	t.Helper()
	storage, err := store.NewRegistryStore(store.RegistryOptions{
		Endpoint:  strings.TrimPrefix(server.URL, "http://"),
		PlainHTTP: true,
	})
	require.NoError(t, err)
	return storage
}
