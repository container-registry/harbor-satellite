package telemetry

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Most API requests are one or two statements; the upper buckets cover
// group syncs that push a state artifact to Harbor.
var latencyBuckets = []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

type promRecorder struct {
	httpRequests  *prometheus.CounterVec
	httpDuration  *prometheus.HistogramVec
	heartbeats    prometheus.Counter
	flushes       *prometheus.CounterVec
	flushDuration *prometheus.HistogramVec
}

var _ Recorder = (*promRecorder)(nil)

func newPromRecorder() *promRecorder {
	return &promRecorder{
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricHTTPRequests,
			Help: "HTTP requests answered, by route template, method and status code.",
		}, []string{"route", "method", "code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricHTTPRequestDuration,
			Help:    "Duration of HTTP requests, by route template and method.",
			Buckets: latencyBuckets,
		}, []string{"route", "method"}),
		heartbeats: prometheus.NewCounter(prometheus.CounterOpts{
			Name: MetricHeartbeats,
			Help: "Satellite heartbeats accepted into this replica's buffer.",
		}),
		flushes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricHeartbeatFlushes,
			Help: "Writes of buffered heartbeats to the database, by outcome.",
		}, []string{"outcome"}),
		flushDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricHeartbeatFlushTime,
			Help:    "Duration of one heartbeat buffer write, by outcome.",
			Buckets: latencyBuckets,
		}, []string{"outcome"}),
	}
}

func (r *promRecorder) collectors() []prometheus.Collector {
	return []prometheus.Collector{r.httpRequests, r.httpDuration, r.heartbeats, r.flushes, r.flushDuration}
}

func (r *promRecorder) ObserveHTTP(route, method string, code int, d time.Duration) {
	r.httpRequests.WithLabelValues(route, method, strconv.Itoa(code)).Inc()
	r.httpDuration.WithLabelValues(route, method).Observe(d.Seconds())
}

func (r *promRecorder) CountHeartbeat() {
	r.heartbeats.Inc()
}

func (r *promRecorder) ObserveHeartbeatFlush(outcome string, d time.Duration) {
	r.flushes.WithLabelValues(outcome).Inc()
	r.flushDuration.WithLabelValues(outcome).Observe(d.Seconds())
}
