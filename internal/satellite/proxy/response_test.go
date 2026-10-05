package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

func TestWriteContentForwardsStoreResponse(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusPartialContent, http.StatusNotModified, http.StatusPreconditionFailed, http.StatusRequestedRangeNotSatisfiable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			body := &responseBody{Reader: strings.NewReader("store response")}
			response := httptest.NewRecorder()
			proxy.New(func(request *proxy.Request) error {
				return request.WriteContent(&http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Range": {"bytes 2-5/10"}},
					Body:       body,
				})
			}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))
			require.Equal(t, status, response.Code)
			require.Equal(t, "bytes 2-5/10", response.Header().Get("Content-Range"))
			require.Equal(t, "store response", response.Body.String())
			require.True(t, body.closed)
		})
	}
}

func TestWriteResponseHeadClosesWithoutReading(t *testing.T) {
	t.Parallel()
	body := &responseBody{Reader: strings.NewReader("unread")}
	response := httptest.NewRecorder()
	proxy.New(func(request *proxy.Request) error {
		return request.WriteResponse(&http.Response{StatusCode: http.StatusOK, Body: body})
	}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/v2/team/app/blobs/sha256:"+strings.Repeat("a", 64), nil))
	require.Empty(t, response.Body.String())
	require.Zero(t, body.reads)
	require.True(t, body.closed)
}

func TestWriteContentStreamsBeforeEOF(t *testing.T) {
	t.Parallel()
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	response := &firstWriteResponse{ResponseRecorder: httptest.NewRecorder(), firstWrite: make(chan struct{})}
	result := make(chan error, 1)
	handler := proxy.New(func(request *proxy.Request) error {
		err := request.WriteContent(&http.Response{StatusCode: http.StatusOK, Body: reader, ContentLength: 10})
		result <- err
		return err
	}).Handler()
	go handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	_, err := writer.Write([]byte("first"))
	require.NoError(t, err)
	select {
	case <-response.firstWrite:
	case <-time.After(time.Second):
		t.Fatal("content was not streamed before source EOF")
	}
	_, err = writer.Write([]byte("later"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, <-result)
	require.Equal(t, "firstlater", response.Body.String())
}

type responseBody struct {
	io.Reader
	reads  int64
	closed bool
}

func (body *responseBody) Read(payload []byte) (int, error) {
	n, err := body.Reader.Read(payload)
	body.reads += int64(n)
	return n, err
}

func (body *responseBody) Close() error { body.closed = true; return nil }

type firstWriteResponse struct {
	*httptest.ResponseRecorder
	firstWrite chan struct{}
	once       sync.Once
}

func (response *firstWriteResponse) Write(payload []byte) (int, error) {
	n, err := response.ResponseRecorder.Write(payload)
	response.once.Do(func() { close(response.firstWrite) })
	return n, err
}
