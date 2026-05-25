package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestMetricsEndpoint(t *testing.T) {
	server, mock := newMockServer(t)
	server.metrics = newMetrics()

	router := server.RegisterRoutes()
	pingReq := httptest.NewRequest(http.MethodGet, "/ping", nil)
	pingRR := httptest.NewRecorder()
	router.ServeHTTP(pingRR, pingReq)
	require.Equal(t, http.StatusOK, pingRR.Code)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "ground_control_http_requests_total")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHTTPMetricsMiddleware(t *testing.T) {
	metrics := newMetrics()
	handler := metrics.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRR, metricsReq)

	body := metricsRR.Body.String()
	require.Contains(t, body, `ground_control_http_requests_total{method="GET",route="unknown",status="202"} 1`)
	require.Contains(t, body, `ground_control_http_request_duration_seconds_count{method="GET",route="unknown"} 1`)
}

func TestSyncMetrics(t *testing.T) {
	metrics := newMetrics()
	router := mux.NewRouter()
	router.Use(metrics.HTTPMiddleware)
	router.HandleFunc("/satellites/sync", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}).Methods(http.MethodPost)

	req := httptest.NewRequest(http.MethodPost, "/satellites/sync", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRR, metricsReq)

	body := metricsRR.Body.String()
	require.Contains(t, body, "ground_control_sync_requests_total 1")
	require.Contains(t, body, "ground_control_sync_failures_total 1")
}

func TestCurrentRouteTemplateUnknown(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/missing", strings.NewReader(""))
	require.Equal(t, "unknown", currentRouteTemplate(req))
}
