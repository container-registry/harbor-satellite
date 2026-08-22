package proxy_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	proxy "github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/stretchr/testify/require"
)

func TestStackComposesLayersInDeclarationOrder(t *testing.T) {
	t.Parallel()

	order := make([]string, 0, 5)
	decorate := func(name string) proxy.Layer {
		return proxy.LayerFunc(func(next proxy.Handler) proxy.Handler {
			return proxy.HandlerFunc(func(contract *proxy.Contract) error {
				require.Equal(t, proxy.Manifest, contract.Operation)
				order = append(order, name+":before")
				err := next.Handle(contract)
				order = append(order, name+":after")
				return err
			})
		})
	}
	terminal := terminalLayer(proxy.HandlerFunc(func(contract *proxy.Contract) error {
		require.Equal(t, "team/app", contract.Repository)
		order = append(order, "terminal")
		contract.Response.WriteHeader(http.StatusNoContent)
		return nil
	}))

	response := serveRequest(
		t,
		httptest.NewRequest(http.MethodGet, "/v2/team/app/manifests/latest", nil),
		decorate("authentication"),
		decorate("policy"),
		terminal,
	)
	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, []string{
		"authentication:before",
		"policy:before",
		"terminal",
		"policy:after",
		"authentication:after",
	}, order)
}

func TestLayersCanMutateShortCircuitAndReject(t *testing.T) {
	t.Parallel()

	t.Run("mutate", func(t *testing.T) {
		t.Parallel()

		resolve := proxy.LayerFunc(func(next proxy.Handler) proxy.Handler {
			return proxy.HandlerFunc(func(contract *proxy.Contract) error {
				contract.Reference = testDigest
				return next.Handle(contract)
			})
		})
		terminal := terminalLayer(proxy.HandlerFunc(func(contract *proxy.Contract) error {
			require.Equal(t, testDigest, contract.Reference)
			return nil
		}))
		serveRequest(t, httptest.NewRequest(
			http.MethodGet,
			"/v2/team/app/manifests/latest",
			nil,
		), resolve, terminal)
	})

	t.Run("short circuit", func(t *testing.T) {
		t.Parallel()

		terminalCalled := false
		terminal := terminalLayer(proxy.HandlerFunc(func(*proxy.Contract) error {
			terminalCalled = true
			return nil
		}))
		cacheHit := proxy.LayerFunc(func(proxy.Handler) proxy.Handler {
			return proxy.HandlerFunc(func(contract *proxy.Contract) error {
				contract.Response.WriteHeader(http.StatusOK)
				return nil
			})
		})
		response := serveRequest(t, httptest.NewRequest(
			http.MethodGet,
			"/v2/team/app/manifests/latest",
			nil,
		), cacheHit, terminal)
		require.Equal(t, http.StatusOK, response.Code)
		require.False(t, terminalCalled)
	})

	t.Run("reject", func(t *testing.T) {
		t.Parallel()

		terminalCalled := false
		terminal := terminalLayer(proxy.HandlerFunc(func(*proxy.Contract) error {
			terminalCalled = true
			return nil
		}))
		reject := proxy.LayerFunc(func(proxy.Handler) proxy.Handler {
			return proxy.HandlerFunc(func(*proxy.Contract) error {
				return proxy.NewError(proxy.ErrorCodeDenied, "policy rejected request", nil)
			})
		})
		response := serveRequest(
			t,
			httptest.NewRequest(http.MethodGet, "/v2/", nil),
			reject,
			terminal,
		)
		require.Equal(t, http.StatusForbidden, response.Code)
		require.False(t, terminalCalled)
	})
}

func TestAdapterStopsBeforeLayersWhenParsingFails(t *testing.T) {
	t.Parallel()

	called := false
	current := proxy.LayerFunc(func(next proxy.Handler) proxy.Handler {
		return proxy.HandlerFunc(func(contract *proxy.Contract) error {
			called = true
			return next.Handle(contract)
		})
	})
	response := serveRequest(t, httptest.NewRequest(
		http.MethodPost,
		"/v2/team/app/manifests/latest",
		nil,
	), current)
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

			fail := proxy.LayerFunc(func(proxy.Handler) proxy.Handler {
				return proxy.HandlerFunc(func(*proxy.Contract) error { return test.err })
			})
			response := serveRequest(
				t,
				httptest.NewRequest(http.MethodGet, "/v2/", nil),
				fail,
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

	fail := proxy.LayerFunc(func(proxy.Handler) proxy.Handler {
		return proxy.HandlerFunc(func(*proxy.Contract) error {
			return proxy.NewError(proxy.ErrorCodeManifestUnknown, "release does not exist", nil)
		})
	})
	response := serveRequest(t, httptest.NewRequest(
		http.MethodHead,
		"/v2/team/app/manifests/latest",
		nil,
	), fail)
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Empty(t, response.Body.String())
	require.NotEmpty(t, response.Header().Get("Content-Length"))
}

func TestAdapterDoesNotInventAnErrorCodeForInternalErrors(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		errors.New("internal"),
		&proxy.DistributionError{Code: proxy.ErrorCode("POLICY_REJECTED"), Message: "denied"},
	} {
		fail := proxy.LayerFunc(func(proxy.Handler) proxy.Handler {
			return proxy.HandlerFunc(func(*proxy.Contract) error { return err })
		})
		response := serveRequest(
			t,
			httptest.NewRequest(http.MethodGet, "/v2/", nil),
			fail,
		)
		require.Equal(t, http.StatusInternalServerError, response.Code)
		require.Equal(t, "Internal Server Error\n", response.Body.String())
	}
}

func TestStackConstruction(t *testing.T) {
	t.Parallel()

	wrapCount := 0
	current := proxy.LayerFunc(func(next proxy.Handler) proxy.Handler {
		wrapCount++
		return next
	})
	stack := requireStack(t, current)
	require.Equal(t, 1, wrapCount)
	stack.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v2/", nil))
	stack.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v2/", nil))
	require.Equal(t, 1, wrapCount)

	empty, err := proxy.New()
	require.NoError(t, err)
	response := httptest.NewRecorder()
	empty.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	require.Equal(t, http.StatusOK, response.Code)
}

func TestNewReturnsCompositionErrors(t *testing.T) {
	t.Parallel()

	_, err := proxy.New(nil)
	require.Error(t, err)
	var typedNil proxy.LayerFunc
	_, err = proxy.New(typedNil)
	require.Error(t, err)
	_, err = proxy.New(proxy.LayerFunc(func(proxy.Handler) proxy.Handler { return nil }))
	require.Error(t, err)
	_, err = proxy.New(proxy.When(nil, proxy.LayerFunc(func(next proxy.Handler) proxy.Handler { return next })))
	require.Error(t, err)
	_, err = proxy.New(proxy.When(proxy.IsPull, nil))
	require.Error(t, err)
}

func TestComposedHTTPHandlerSupportsConcurrentRequests(t *testing.T) {
	t.Parallel()

	terminal := terminalLayer(proxy.HandlerFunc(func(contract *proxy.Contract) error {
		if contract.Operation != proxy.Manifest || contract.Access != proxy.ReadAccess {
			return errors.New("unexpected operation")
		}
		contract.Response.WriteHeader(http.StatusNoContent)
		return nil
	}))
	handler := requireStack(t, proxy.When(proxy.IsPull, proxy.LayerFunc(
		func(next proxy.Handler) proxy.Handler { return next },
	)), terminal).Handler()

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
