package peer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

func TestNormalizeAddr(t *testing.T) {
	t.Parallel()
	require.Equal(t, ":5000", NormalizeAddr("5000"))
	require.Equal(t, ":5000", NormalizeAddr(":5000"))
	require.Equal(t, "127.0.0.1:5000", NormalizeAddr("127.0.0.1:5000"))
	require.Empty(t, NormalizeAddr("  "))
}

func TestSplitURLs(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"http://a:5000", "http://b:5000"}, SplitURLs("http://a:5000, http://b:5000"))
	require.Nil(t, SplitURLs("  "))
}

func TestReplicaProxyServesDigestAndRejectsWrites(t *testing.T) {
	t.Parallel()

	layout, manifestDesc, layerDesc := seedLayout(t)
	handler := Handler(layout)

	response := serve(t, handler, http.MethodGet, "/v2/")
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, distributionAPIVersion, response.Header().Get("Docker-Distribution-API-Version"))

	manifestPath := "/v2/library/alpine/manifests/" + manifestDesc.Digest.String()
	got := serve(t, handler, http.MethodGet, manifestPath)
	require.Equal(t, http.StatusOK, got.Code)
	require.Equal(t, manifestDesc.MediaType, got.Header().Get("Content-Type"))
	require.Equal(t, manifestDesc.Digest.String(), got.Header().Get("Docker-Content-Digest"))
	require.Equal(t, int(manifestDesc.Size), got.Body.Len())

	head := serve(t, handler, http.MethodHead, manifestPath)
	require.Equal(t, http.StatusOK, head.Code)
	require.Empty(t, head.Body.Bytes())

	blob := serve(t, handler, http.MethodGet, "/v2/library/alpine/blobs/"+layerDesc.Digest.String())
	require.Equal(t, http.StatusOK, blob.Code)
	require.Equal(t, "layer-bytes", blob.Body.String())

	missing := serve(t, handler, http.MethodGet, "/v2/library/alpine/manifests/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.Equal(t, http.StatusNotFound, missing.Code)

	post := serve(t, handler, http.MethodPost, "/v2/library/alpine/blobs/uploads/")
	require.Equal(t, http.StatusMethodNotAllowed, post.Code)

	put := serve(t, handler, http.MethodPut, manifestPath)
	require.Equal(t, http.StatusMethodNotAllowed, put.Code)
}

func TestReplicaProxyUnknownTagIsNotFound(t *testing.T) {
	t.Parallel()
	layout, err := oci.New(t.TempDir())
	require.NoError(t, err)
	response := serve(t, Handler(layout), http.MethodGet, "/v2/library/alpine/manifests/latest")
	require.Equal(t, http.StatusNotFound, response.Code)
}

func seedLayout(t *testing.T) (*oci.Store, ocispec.Descriptor, ocispec.Descriptor) {
	t.Helper()
	layout, err := oci.New(t.TempDir())
	require.NoError(t, err)

	ctx := context.Background()
	layer := []byte("layer-bytes")
	layerDesc := content.NewDescriptorFromBytes("application/vnd.example.layer", layer)
	config := []byte(`{"architecture":"amd64"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, config)
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestBytes)

	require.NoError(t, layout.Push(ctx, layerDesc, bytes.NewReader(layer)))
	require.NoError(t, layout.Push(ctx, configDesc, bytes.NewReader(config)))
	require.NoError(t, layout.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes)))
	return layout, manifestDesc, layerDesc
}

func serve(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	handler.ServeHTTP(response, request)
	_, _ = io.Copy(io.Discard, response.Result().Body)
	return response
}
