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
	heartbeats          *prometheus.CounterVec
	journalAcks         *prometheus.CounterVec
	journalAckedSeq     prometheus.Gauge
	heartbeatsTooLarge  *prometheus.CounterVec
}

var _ Recorder = (*promRecorder)(nil)

func newPromRecorder() *promRecorder {
	return &promRecorder{
		pullDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    MetricPullDuration,
			Help:    "Duration of proxy pull requests including the body transfer, by resource and outcome.",
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
		heartbeats: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricHeartbeats,
			Help: "Heartbeats sent to Ground Control, by HTTP status of the answer, or error without a readable answer.",
		}, []string{"code"}),
		journalAcks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricJournalAcks,
			Help: "Ground Control answers to journal uploads, by outcome (accepted, rejected, dropped).",
		}, []string{"outcome"}),
		journalAckedSeq: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: MetricJournalAckedSeq,
			Help: "The journal seq Ground Control last acknowledged holding.",
		}),
		heartbeatsTooLarge: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: MetricHeartbeatsTooLarge,
			Help: "Heartbeats Ground Control refused with 413, by reaction (halved_batch, without_journal, none).",
		}, []string{"reaction"}),
	}
}

func (r *promRecorder) register(reg prometheus.Registerer) error {
	for _, c := range []prometheus.Collector{r.pullDuration, r.pullThroughFills, r.replicationBytes, r.replicationDuration, r.storeLockWait, r.heartbeats, r.journalAcks, r.journalAckedSeq, r.heartbeatsTooLarge} {
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

func (r *promRecorder) CountHeartbeat(code string) {
	r.heartbeats.WithLabelValues(code).Inc()
}

func (r *promRecorder) ObserveJournalAck(outcome string, ackedSeq int64) {
	r.journalAcks.WithLabelValues(outcome).Inc()
	r.journalAckedSeq.Set(float64(ackedSeq))
}

func (r *promRecorder) CountHeartbeatTooLarge(reaction string) {
	r.heartbeatsTooLarge.WithLabelValues(reaction).Inc()
}
