package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	gcmiddleware "github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddleware(t *testing.T) {
	t.Run("preserves a valid request ID", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/ping", nil)
		request.Header.Set("X-Request-ID", "request-123")
		response := httptest.NewRecorder()

		RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			require.Equal(t, "request-123", r.Header.Get("X-Request-ID"))
		})).ServeHTTP(response, request)

		require.Equal(t, "request-123", response.Header().Get("X-Request-ID"))
	})

	t.Run("replaces an unsafe request ID", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/ping", nil)
		request.Header.Set("X-Request-ID", "unsafe request id")
		response := httptest.NewRecorder()

		RequestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			require.True(t, validAuditRequestID(r.Header.Get("X-Request-ID")))
			require.NotEqual(t, "unsafe request id", r.Header.Get("X-Request-ID"))
		})).ServeHTTP(response, request)

		require.True(t, validAuditRequestID(response.Header().Get("X-Request-ID")))
	})
}

func TestRateLimitMiddleware(t *testing.T) {
	newMockHandlerService(t)
	limiter := gcmiddleware.NewRateLimiter(1, time.Minute)
	serviceInst.rateLimiter = limiter
	t.Cleanup(limiter.Close)

	handled := 0
	handler := RateLimitMiddleware(auditlog.OpLogin, auditlog.ResSession, "login")(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handled++ }),
	)

	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, httptest.NewRequest(http.MethodPost, "/login", nil))
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, httptest.NewRequest(http.MethodPost, "/login", nil))

	require.Equal(t, 1, handled)
	require.Equal(t, http.StatusTooManyRequests, secondResponse.Code)
	require.JSONEq(t, `{"code":429,"message":"Too many requests"}`, secondResponse.Body.String())
}
