package store

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
)

func TestRegistryStoreFetchForwardsRangesAndConditions(t *testing.T) {
	t.Parallel()
	payload := []byte("0123456789")
	descriptor := content.NewDescriptorFromBytes("application/octet-stream", payload)
	etag := strconv.Quote(descriptor.Digest.String())
	for _, resource := range []PullResource{PullResourceManifest, PullResourceBlob} {
		for _, scenario := range []struct {
			name         string
			headers      http.Header
			status       int
			body         string
			contentRange string
		}{
			{name: "full", status: http.StatusOK, body: "0123456789"},
			{name: "single", headers: http.Header{"Range": {"bytes=2-5"}}, status: http.StatusPartialContent, body: "2345", contentRange: "bytes 2-5/10"},
			{name: "suffix", headers: http.Header{"Range": {"bytes=-3"}}, status: http.StatusPartialContent, body: "789", contentRange: "bytes 7-9/10"},
			{name: "resume", headers: http.Header{"Range": {"bytes=7-"}}, status: http.StatusPartialContent, body: "789", contentRange: "bytes 7-9/10"},
			{name: "not modified", headers: http.Header{"If-None-Match": {etag}}, status: http.StatusNotModified},
			{name: "precondition failed", headers: http.Header{"If-Match": {`"other"`}}, status: http.StatusPreconditionFailed},
			{name: "if range matches", headers: http.Header{"Range": {"bytes=2-5"}, "If-Range": {etag}}, status: http.StatusPartialContent, body: "2345", contentRange: "bytes 2-5/10"},
			{name: "if range mismatches", headers: http.Header{"Range": {"bytes=2-5"}, "If-Range": {`"other"`}}, status: http.StatusOK, body: "0123456789"},
			{name: "unsatisfiable", headers: http.Header{"Range": {"bytes=20-"}}, status: http.StatusRequestedRangeNotSatisfiable, body: "invalid range: failed to overlap\n", contentRange: "bytes */10"},
		} {
			t.Run(strconv.Itoa(int(resource))+"/"+scenario.name, func(t *testing.T) {
				t.Parallel()
				path := "blobs"
				if resource == PullResourceManifest {
					path = "manifests"
				}
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
					if _, password, ok := request.BasicAuth(); !ok || password != "secret" {
						response.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
						response.WriteHeader(http.StatusUnauthorized)
						return
					}
					require.Equal(t, "/v2/team/app/"+path+"/"+descriptor.Digest.String(), request.URL.Path)
					require.Equal(t, "identity", request.Header.Get("Accept-Encoding"))
					require.Empty(t, request.Header.Get("Cookie"))
					for name, values := range scenario.headers {
						require.Equal(t, values, request.Header.Values(name))
					}
					response.Header().Set("Etag", etag)
					http.ServeContent(response, request, "content", time.Time{}, bytes.NewReader(payload))
				}))
				t.Cleanup(server.Close)
				storage, err := NewRegistryStore(RegistryOptions{Endpoint: server.URL, PlainHTTP: true, Username: "robot", Password: "secret"})
				require.NoError(t, err)
				headers := scenario.headers.Clone()
				if headers == nil {
					headers = make(http.Header)
				}
				headers.Set("Authorization", "Bearer client-secret")
				headers.Set("Cookie", "session=client-secret")
				response, err := storage.Fetch(context.Background(), Artifact{Name: "team/app"}, descriptor, resource, headers)
				require.NoError(t, err)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.Equal(t, scenario.status, response.StatusCode)
				require.Equal(t, scenario.body, string(body))
				require.Equal(t, scenario.contentRange, response.Header.Get("Content-Range"))
				if response.StatusCode == http.StatusPartialContent {
					require.Equal(t, int64(len(body)), response.ContentLength)
				}
			})
		}
	}
}

func TestRegistryStoreFetchStreamsMultipartRangesInRequestedOrder(t *testing.T) {
	t.Parallel()
	payload := []byte("0123456789")
	descriptor := content.NewDescriptorFromBytes("application/octet-stream", payload)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "bytes=7-8,0-1", request.Header.Get("Range"))
		http.ServeContent(response, request, "blob", time.Time{}, bytes.NewReader(payload))
	}))
	t.Cleanup(server.Close)
	storage, err := NewRegistryStore(RegistryOptions{Endpoint: server.URL, PlainHTTP: true})
	require.NoError(t, err)
	response, err := storage.Fetch(context.Background(), Artifact{Name: "team/app"}, descriptor, PullResourceBlob, http.Header{"Range": {"bytes=7-8,0-1"}})
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusPartialContent, response.StatusCode)
	mediaType, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/byteranges", mediaType)
	parts := multipart.NewReader(response.Body, parameters["boundary"])
	for _, expected := range []string{"78", "01"} {
		part, err := parts.NextPart()
		require.NoError(t, err)
		body, err := io.ReadAll(part)
		require.NoError(t, err)
		require.NoError(t, part.Close())
		require.Equal(t, expected, string(body))
	}
	_, err = parts.NextPart()
	require.ErrorIs(t, err, io.EOF)
}

func TestRegistryStoreFetchStreamsBeforeEOFAndCancelsWithRequest(t *testing.T) {
	t.Parallel()
	payload := []byte("firstlater")
	descriptor := content.NewDescriptorFromBytes("application/octet-stream", payload)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Length", "10")
		_, err := response.Write(payload[:5])
		require.NoError(t, err)
		require.NoError(t, http.NewResponseController(response).Flush())
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	storage, err := NewRegistryStore(RegistryOptions{Endpoint: server.URL, PlainHTTP: true})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response, err := storage.Fetch(ctx, Artifact{Name: "team/app"}, descriptor, PullResourceBlob, nil)
	require.NoError(t, err)
	defer response.Body.Close()
	first := make([]byte, 5)
	_, err = io.ReadFull(response.Body, first)
	require.NoError(t, err)
	require.Equal(t, payload[:5], first)
	cancel()
	_, err = io.ReadAll(response.Body)
	require.ErrorIs(t, err, context.Canceled)
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
	response, err := destination.Fetch(context.Background(), artifact, descriptor, PullResourceManifest, nil)
	require.NoError(t, err)
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
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
