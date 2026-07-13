package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/health"
	"github.com/stretchr/testify/require"
)

func TestHealth(t *testing.T) {
	t.Run("healthy database", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectPing()

		responder := Health(health.HealthParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/health"),
		})

		response, ok := responder.(*health.HealthOK)
		require.True(t, ok)
		require.Equal(t, "healthy", response.Payload.Status)
	})

	t.Run("unavailable database", func(t *testing.T) {
		mock := newMockHandlerService(t)
		mock.ExpectPing().WillReturnError(errors.New("database unavailable"))

		responder := Health(health.HealthParams{
			HTTPRequest: handlerRequest(http.MethodGet, "/health"),
		})

		response, ok := responder.(*health.HealthServiceUnavailable)
		require.True(t, ok)
		require.Equal(t, "unhealthy", response.Payload.Status)
	})
}

func TestPing(t *testing.T) {
	t.Parallel()

	response, ok := Ping(health.PingParams{}).(*health.PingOK)
	require.True(t, ok)
	require.Equal(t, "pong", response.Payload)
}
