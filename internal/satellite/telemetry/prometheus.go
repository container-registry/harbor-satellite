package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Pull and lock waits are mostly local disk reads; the upper buckets cover
// pull-through fills from a distant Harbor.
var latencyBuckets = []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300}

type promRecorder struct {
	pullDuration        *prometheus.HistogramVec
	pullThroughFills    *prometheus.CounterVec
	replicationBytes    *prometheus.CounterVec
	replicationDuration *prometheus.HistogramVec
	storeLockWait       *prometheus.HistogramVec
}

var _ Recorder = (*promRecorder)(nil)

func newPromRecorder() *promRecorder {
	return &promRecorder{
		pullDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricPullDuration,
			Help:    "Duration of proxy pulls until the response starts, by resource and outcome.",
			Buckets: latencyBuckets,
		}, []string{"resource", "outcome"}),
		pullThroughFills: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricPullThroughFills,
			Help: "Proxy-mode fills of a local miss from Harbor, by outcome.",
		}, []string{"outcome"}),
		replicationBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricReplicationBytes,
			Help: "Bytes replicated into the local store, by outcome.",
		}, []string{"outcome"}),
		replicationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricReplicationDuration,
			Help:    "Duration of one replication run, by outcome.",
			Buckets: latencyBuckets,
		}, []string{"outcome"}),
		storeLockWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricStoreLockWait,
			Help:    "Time spent waiting for the local store's index lock, by operation.",
			Buckets: latencyBuckets,
		}, []string{"operation"}),
	}
}

func (r *promRecorder) register(reg prometheus.Registerer) error {
	for _, c := range []prometheus.Collector{r.pullDuration, r.pullThroughFills, r.replicationBytes, r.replicationDuration, r.storeLockWait} {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

func (r *promRecorder) ObservePull(resource, outcome string, d time.Duration) {
	r.pullDuration.WithLabelValues(resource, outcome).Observe(d.Seconds())
}

func (r *promRecorder) CountPullThroughFill(outcome string) {
	r.pullThroughFills.WithLabelValues(outcome).Inc()
}

func (r *promRecorder) ObserveReplication(bytes int64, d time.Duration, outcome string) {
	r.replicationBytes.WithLabelValues(outcome).Add(float64(bytes))
	r.replicationDuration.WithLabelValues(outcome).Observe(d.Seconds())
}

func (r *promRecorder) ObserveStoreLockWait(operation string, d time.Duration) {
	r.storeLockWait.WithLabelValues(operation).Observe(d.Seconds())
}
