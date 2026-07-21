package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	groundcontrolmiddleware "github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
	"github.com/stretchr/testify/require"
)

func TestGeneratedRoutes(t *testing.T) {
	server := &Server{
		rateLimiter: groundcontrolmiddleware.NewRateLimiter(10, time.Minute),
	}
	handler := server.RegisterRoutes()

	t.Run("public endpoint", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/ping", nil)

		handler.ServeHTTP(response, request)

		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "pong", response.Body.String())
		require.NotEmpty(t, response.Header().Get("X-Request-ID"))
	})

	t.Run("protected endpoint", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/configs", nil)

		handler.ServeHTTP(response, request)

		require.Equal(t, http.StatusUnauthorized, response.Code)
	})

	t.Run("generated method and request body contract", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/satellites/ztr", strings.NewReader("{"))
		request.Header.Set("Content-Type", "application/json")

		handler.ServeHTTP(response, request)

		require.Equal(t, http.StatusBadRequest, response.Code)
	})
}
