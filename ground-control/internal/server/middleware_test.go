package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddleware_GeneratesWhenAbsent(t *testing.T) {
	var s Server
	var seen string
	h := s.RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	require.NotEmpty(t, seen, "a request ID should be generated when none is supplied")
	require.Equal(t, seen, rec.Header().Get("X-Request-ID"), "generated ID should be echoed on the response")
}

func TestRequestIDMiddleware_ReusesInboundHeader(t *testing.T) {
	var s Server
	var seen string
	h := s.RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, "abc-123", seen, "an inbound X-Request-ID should be reused")
	require.Equal(t, "abc-123", rec.Header().Get("X-Request-ID"))
}

func TestRequestIDFromContext_DefaultsToEmpty(t *testing.T) {
	require.Empty(t, requestIDFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()))
}
