package store

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/peer"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/oci"
)

func TestCopyFromUsesHarborDestinationReference(t *testing.T) {
	source := newTestRegistry(t)
	image := pushImage(t, source, "alpine", "latest", 1)
	digest, err := image.Digest()
	require.NoError(t, err)

	peerStore, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: source, PlainHTTP: true})
	require.NoError(t, err)
	artifact := Artifact{Name: "alpine", Repository: "library", Tag: "latest", Digest: digest.String()}
	require.NoError(t, peerStore.Replicate(testContext(), []Artifact{artifact}))

	server := httptest.NewServer(peer.Handler(peerStore.Layout()))
	t.Cleanup(server.Close)
	peerURL := strings.TrimPrefix(server.URL, "http://")

	const harbor = "harbor.example.com"
	dest, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: harbor})
	require.NoError(t, err)
	require.NoError(t, dest.CopyFrom(testContext(), RegistryOptions{Endpoint: peerURL, PlainHTTP: true}, []Artifact{artifact}))

	got, err := dest.Layout().Resolve(testContext(), harbor+"/library/alpine:latest")
	require.NoError(t, err)
	require.Equal(t, digest.String(), got.Digest.String())

	_, err = dest.Layout().Resolve(testContext(), peerURL+"/library/alpine:latest")
	require.Error(t, err)
}

func TestPeerStoreCopiesFromFirstDigestHit(t *testing.T) {
	// Concurrent Resolve: empty peer is skipped immediately; the hit wins.
	// Layout tags stay the Harbor destination reference.
	source := newTestRegistry(t)
	image := pushImage(t, source, "alpine", "latest", 1)
	digest, err := image.Digest()
	require.NoError(t, err)
	artifact := Artifact{Name: "alpine", Repository: "library", Tag: "latest", Digest: digest.String()}

	filled, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: source, PlainHTTP: true})
	require.NoError(t, err)
	require.NoError(t, filled.Replicate(testContext(), []Artifact{artifact}))

	emptyLayout, err := oci.New(t.TempDir())
	require.NoError(t, err)
	miss := httptest.NewServer(peer.Handler(emptyLayout))
	t.Cleanup(miss.Close)
	hit := httptest.NewServer(peer.Handler(filled.Layout()))
	t.Cleanup(hit.Close)

	const harbor = "harbor.example.com"
	dest, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: harbor})
	require.NoError(t, err)
	peers := []RegistryOptions{
		{Endpoint: strings.TrimPrefix(miss.URL, "http://"), PlainHTTP: true},
		{Endpoint: strings.TrimPrefix(hit.URL, "http://"), PlainHTTP: true},
	}
	require.NoError(t, NewPeerStore(dest, peers).Replicate(testContext(), []Artifact{artifact}))

	got, err := dest.Layout().Resolve(testContext(), harbor+"/library/alpine:latest")
	require.NoError(t, err)
	require.Equal(t, digest.String(), got.Digest.String())
}

func TestPeerStoreFallsBackToHarbor(t *testing.T) {
	// Miss on every peer skips locally and copies from Harbor.
	source := newTestRegistry(t)
	image := pushImage(t, source, "alpine", "latest", 1)
	digest, err := image.Digest()
	require.NoError(t, err)
	artifact := Artifact{Name: "alpine", Repository: "library", Tag: "latest", Digest: digest.String()}

	emptyLayout, err := oci.New(t.TempDir())
	require.NoError(t, err)
	miss := httptest.NewServer(peer.Handler(emptyLayout))
	t.Cleanup(miss.Close)

	dest, err := NewOCIStore(t.TempDir(), RegistryOptions{Endpoint: source, PlainHTTP: true})
	require.NoError(t, err)
	peers := []RegistryOptions{{Endpoint: strings.TrimPrefix(miss.URL, "http://"), PlainHTTP: true}}
	require.NoError(t, NewPeerStore(dest, peers).Replicate(testContext(), []Artifact{artifact}))

	got, err := dest.Layout().Resolve(testContext(), source+"/library/alpine:latest")
	require.NoError(t, err)
	require.Equal(t, digest.String(), got.Digest.String())
}

func TestPlainHTTPFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		raw          string
		useUnsecure  bool
		wantPlainHTTP bool
	}{
		{name: "https wins over use-unsecure", raw: "https://demo.goharbor.io", useUnsecure: true, wantPlainHTTP: false},
		{name: "http is plain", raw: "http://satellite-a:5000", useUnsecure: false, wantPlainHTTP: true},
		{name: "schemeless follows use-unsecure", raw: "demo.goharbor.io", useUnsecure: true, wantPlainHTTP: true},
		{name: "schemeless secure", raw: "demo.goharbor.io", useUnsecure: false, wantPlainHTTP: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantPlainHTTP, PlainHTTPFromURL(tt.raw, tt.useUnsecure))
		})
	}
}

