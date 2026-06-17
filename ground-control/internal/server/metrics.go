package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry            *prometheus.Registry
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	syncRequestsTotal   prometheus.Counter
	syncFailuresTotal   prometheus.Counter
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func newMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		registry: registry,
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ground_control_http_requests_total",
				Help: "Total number of HTTP requests handled by Ground Control.",
			},
			[]string{"method", "route", "status"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ground_control_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds for Ground Control.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
		syncRequestsTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "ground_control_sync_requests_total",
				Help: "Total number of satellite sync requests received by Ground Control.",
			},
		),
		syncFailuresTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "ground_control_sync_failures_total",
				Help: "Total number of failed satellite sync requests handled by Ground Control.",
			},
		),
	}

	registry.MustRegister(
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.syncRequestsTotal,
		m.syncFailuresTotal,
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(recorder, r)

		route := currentRouteTemplate(r)
		status := strconv.Itoa(recorder.statusCode)
		m.httpRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		m.httpRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		if route == "/satellites/sync" {
			m.RecordSyncRequest()
			if recorder.statusCode >= http.StatusBadRequest {
				m.RecordSyncFailure()
			}
		}
	})
}

func (m *Metrics) RecordSyncRequest() {
	m.syncRequestsTotal.Inc()
}

func (m *Metrics) RecordSyncFailure() {
	m.syncFailuresTotal.Inc()
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func currentRouteTemplate(r *http.Request) string {
	route := mux.CurrentRoute(r)
	if route == nil {
		return "unknown"
	}
	template, err := route.GetPathTemplate()
	if err != nil || template == "" {
		return "unknown"
	}
	return template
}
