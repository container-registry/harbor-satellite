package proxy_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type errorEnvelope struct {
	Errors []struct {
		Code    proxy.ErrorCode `json:"code"`
		Message string          `json:"message"`
	} `json:"errors"`
}

func TestNewUsesDefaultServeMuxWithoutProcess(t *testing.T) {
	original := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	t.Cleanup(func() { http.DefaultServeMux = original })

	http.DefaultServeMux.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
	adapter := proxy.New(nil)
	_, implementsHTTPHandler := any(adapter).(http.Handler)
	require.False(t, implementsHTTPHandler)
	response := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestNewInstallsProcess(t *testing.T) {
	t.Parallel()

	var captured *proxy.Request
	adapter := proxy.New(proxy.Process(func(requestState *proxy.Request) error {
		captured = requestState
		return requestState.Write(http.StatusNoContent, nil)
	}))
	handler := adapter.Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/v2/team/app/manifests/latest",
		nil,
	))

	require.Equal(t, http.StatusNoContent, response.Code)
	require.NotNil(t, captured)
	require.Equal(t, proxy.PullManifest, captured.Operation)
	require.Equal(t, "team/app", captured.Repository)
}

func TestWrapComposesProcessorsInExecutionOrder(t *testing.T) {
	t.Parallel()

	order := make([]string, 0, 5)
	process := func(requestState *proxy.Request) error {
		order = append(order, "handler")
		return requestState.Write(http.StatusNoContent, nil)
	}
	logger := func(next proxy.Process) proxy.Process {
		return func(requestState *proxy.Request) error {
			order = append(order, "logger:before")
			err := next(requestState)
			order = append(order, "logger:after")
			return err
		}
	}
	auth := func(next proxy.Process) proxy.Process {
		return func(requestState *proxy.Request) error {
			order = append(order, "auth:before")
			err := next(requestState)
			order = append(order, "auth:after")
			return err
		}
	}

	response := httptest.NewRecorder()
	proxy.New(process).
		Wrap(auth).
		Wrap(logger).
		Handler().
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, []string{
		"logger:before",
		"auth:before",
		"handler",
		"auth:after",
		"logger:after",
	}, order)
}

func TestWrapAllComposesProcessorsInArgumentOrder(t *testing.T) {
	t.Parallel()

	order := make([]string, 0, 5)
	process := func(requestState *proxy.Request) error {
		order = append(order, "process")
		return requestState.Write(http.StatusNoContent, nil)
	}
	first := func(next proxy.Process) proxy.Process {
		return func(requestState *proxy.Request) error {
			order = append(order, "first:before")
			err := next(requestState)
			order = append(order, "first:after")
			return err
		}
	}
	second := func(next proxy.Process) proxy.Process {
		return func(requestState *proxy.Request) error {
			order = append(order, "second:before")
			err := next(requestState)
			order = append(order, "second:after")
			return err
		}
	}

	response := httptest.NewRecorder()
	proxy.New(process).
		WrapAll(first, nil, second).
		Handler().
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, []string{
		"second:before",
		"first:before",
		"process",
		"first:after",
		"second:after",
	}, order)
}

func TestWrittenResponseStopsProcessChain(t *testing.T) {
	t.Parallel()

	innerCalled := false
	inner := proxy.Process(func(*proxy.Request) error {
		innerCalled = true
		return nil
	})
	outer := proxy.Processor(func(proxy.Process) proxy.Process {
		return proxy.Process(func(requestState *proxy.Request) error {
			return requestState.Write(http.StatusAccepted, []byte("complete"))
		})
	})

	response := httptest.NewRecorder()
	proxy.New(inner).
		Wrap(outer).
		Handler().
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, "complete", response.Body.String())
	require.False(t, innerCalled)
}

func TestProcessorErrorStopsInnerProcess(t *testing.T) {
	t.Parallel()

	innerCalled := false
	inner := proxy.Process(func(*proxy.Request) error {
		innerCalled = true
		return nil
	})
	outer := proxy.Processor(func(proxy.Process) proxy.Process {
		return proxy.Process(func(*proxy.Request) error {
			return proxy.NewError(proxy.ErrorCodeDenied, "policy denied request", nil)
		})
	})

	response := httptest.NewRecorder()
	proxy.New(inner).
		WrapAll(outer).
		Handler().
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))

	require.Equal(t, http.StatusForbidden, response.Code)
	require.False(t, innerCalled)
}

func TestNilProcessorsLeaveCurrentProcessUnchanged(t *testing.T) {
	t.Parallel()

	called := false
	adapter := proxy.New(proxy.Process(func(*proxy.Request) error {
		called = true
		return nil
	}))
	handler := adapter.Wrap(nil).WrapAll(nil, nil).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))

	require.True(t, called)
}

func TestHandlerCapturesComposedProcess(t *testing.T) {
	t.Parallel()

	wrapped := false
	adapter := proxy.New(func(requestState *proxy.Request) error {
		return requestState.Write(http.StatusNoContent, nil)
	})
	handler := adapter.Handler()
	adapter.Wrap(func(next proxy.Process) proxy.Process {
		return func(requestState *proxy.Request) error {
			wrapped = true
			return next(requestState)
		}
	})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))

	require.Equal(t, http.StatusNoContent, response.Code)
	require.False(t, wrapped)
}

func TestAdapterStopsBeforeProcessWhenParsingFails(t *testing.T) {
	t.Parallel()

	called := false
	response := serveRequest(t, httptest.NewRequest(
		http.MethodPost,
		"/v2/team/app/manifests/latest",
		nil,
	), proxy.Process(func(*proxy.Request) error {
		called = true
		return nil
	}))

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	require.False(t, called)
	require.Equal(t, proxy.ErrorCodeUnsupported, decodeError(t, response).Errors[0].Code)
}

func TestAdapterWritesDistributionErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		code    proxy.ErrorCode
		message string
		status  int
	}{
		{"unauthorized", proxy.NewError(proxy.ErrorCodeUnauthorized, "bearer token required", nil), proxy.ErrorCodeUnauthorized, "bearer token required", http.StatusUnauthorized},
		{"denied", proxy.NewError(proxy.ErrorCodeDenied, "policy denied request", nil), proxy.ErrorCodeDenied, "policy denied request", http.StatusForbidden},
		{"manifest unknown", proxy.NewError(proxy.ErrorCodeManifestUnknown, "release does not exist", nil), proxy.ErrorCodeManifestUnknown, "release does not exist", http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := serveRequest(
				t,
				httptest.NewRequest(http.MethodGet, "/v2/", nil),
				proxy.Process(func(*proxy.Request) error { return test.err }),
			)
			envelope := decodeError(t, response)
			require.Equal(t, test.status, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			require.Equal(t, test.code, envelope.Errors[0].Code)
			require.Equal(t, test.message, envelope.Errors[0].Message)
		})
	}
}

func TestAdapterOmitsDistributionErrorBodyForHead(t *testing.T) {
	t.Parallel()

	response := serveRequest(t, httptest.NewRequest(
		http.MethodHead,
		"/v2/team/app/manifests/latest",
		nil,
	), proxy.Process(func(*proxy.Request) error {
		return proxy.NewError(proxy.ErrorCodeManifestUnknown, "release does not exist", nil)
	}))

	require.Equal(t, http.StatusNotFound, response.Code)
	require.Empty(t, response.Body.String())
	require.NotEmpty(t, response.Header().Get("Content-Length"))
}

func TestAdapterDoesNotInventAnErrorCodeForInternalErrors(t *testing.T) {
	t.Parallel()

	var typedNil *proxy.DistributionError
	for _, handlerError := range []error{
		errors.New("internal"),
		&proxy.DistributionError{Code: proxy.ErrorCode("POLICY_REJECTED"), Message: "denied"},
		typedNil,
	} {
		response := serveRequest(
			t,
			httptest.NewRequest(http.MethodGet, "/v2/", nil),
			proxy.Process(func(*proxy.Request) error { return handlerError }),
		)
		require.Equal(t, http.StatusInternalServerError, response.Code)
		require.Equal(t, "Internal Server Error\n", response.Body.String())
	}
}

func TestAdapterDoesNotAppendAnErrorAfterResponseCommit(t *testing.T) {
	t.Parallel()

	response := serveRequest(
		t,
		httptest.NewRequest(http.MethodGet, "/v2/", nil),
		proxy.Process(func(requestState *proxy.Request) error {
			require.NoError(t, requestState.Write(
				http.StatusAccepted,
				[]byte("response started"),
			))
			return proxy.NewError(proxy.ErrorCodeDenied, "too late to serialize", nil)
		}),
	)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, "response started", response.Body.String())
	require.NotContains(t, response.Body.String(), "errors")
}

func TestRequestWriteResponseForwardsHTTPResponse(t *testing.T) {
	t.Parallel()

	response := serveRequest(
		t,
		httptest.NewRequest(http.MethodGet, "/v2/", nil),
		proxy.Process(func(requestState *proxy.Request) error {
			return requestState.WriteResponse(&http.Response{
				StatusCode: http.StatusAccepted,
				Header: http.Header{
					"Content-Type": []string{"application/vnd.oci.image.manifest.v1+json"},
				},
				Body: io.NopCloser(strings.NewReader("manifest")),
			})
		}),
	)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Equal(t, "application/vnd.oci.image.manifest.v1+json", response.Header().Get("Content-Type"))
	require.Equal(t, "manifest", response.Body.String())
}

func TestRequestWriteResponseRemovesHopByHopHeaders(t *testing.T) {
	t.Parallel()

	response := serveRequest(
		t,
		httptest.NewRequest(http.MethodGet, "/v2/", nil),
		proxy.Process(func(requestState *proxy.Request) error {
			return requestState.WriteResponse(&http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Connection":        []string{"keep-alive, X-Upstream-Hop"},
					"Keep-Alive":        []string{"timeout=5"},
					"Transfer-Encoding": []string{"chunked"},
					"X-Upstream-Hop":    []string{"remove-me"},
					"X-Registry":        []string{"retain-me"},
				},
				Body: io.NopCloser(strings.NewReader("content")),
			})
		}),
	)

	require.Empty(t, response.Header().Get("Connection"))
	require.Empty(t, response.Header().Get("Keep-Alive"))
	require.Empty(t, response.Header().Get("Transfer-Encoding"))
	require.Empty(t, response.Header().Get("X-Upstream-Hop"))
	require.Equal(t, "retain-me", response.Header().Get("X-Registry"))
	require.Equal(t, "content", response.Body.String())
}

func TestHTTPHandlerSupportsConcurrentRequests(t *testing.T) {
	t.Parallel()

	handler := proxy.New(proxy.Process(func(requestState *proxy.Request) error {
		if requestState.Operation != proxy.PullManifest || requestState.Method != proxy.GET {
			return errors.New("unexpected operation")
		}
		return requestState.Write(http.StatusNoContent, nil)
	})).Handler()

	const requestCount = 32
	results := make(chan int, requestCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for range requestCount {
		go func() {
			defer waitGroup.Done()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(
				http.MethodGet,
				"/v2/team/app/manifests/latest",
				nil,
			))
			results <- response.Code
		}()
	}
	waitGroup.Wait()
	close(results)
	for status := range results {
		require.Equal(t, http.StatusNoContent, status)
	}
}

func serveRequest(
	t *testing.T,
	request *http.Request,
	processes ...proxy.Process,
) *httptest.ResponseRecorder {
	t.Helper()
	require.LessOrEqual(t, len(processes), 1)
	process := proxy.Process(func(*proxy.Request) error {
		return nil
	})
	if len(processes) != 0 {
		process = processes[0]
	}

	response := httptest.NewRecorder()
	proxy.New(process).Handler().ServeHTTP(response, request)
	return response
}

func decodeError(t *testing.T, response *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var envelope errorEnvelope
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Len(t, envelope.Errors, 1)
	return envelope
}

func requireDistributionResponse(
	t *testing.T,
	request *http.Request,
	code proxy.ErrorCode,
	status int,
) {
	t.Helper()
	response := serveRequest(t, request)
	require.Equal(t, status, response.Code)
	require.Equal(t, code, decodeError(t, response).Errors[0].Code)
}
