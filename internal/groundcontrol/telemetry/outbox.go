package telemetry

import (
	"context"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// OutboxCounts is the outbox at one scrape: Pending events still have
// attempts left, Failed ran out of them, OldestPending is the age of the
// oldest event that is not done (zero without one).
type OutboxCounts struct {
	Pending, Failed int64
	OldestPending   time.Duration
}

// OutboxStats reads OutboxCounts from the database at scrape time, so
// every replica reports the shared outbox whichever worker drains it.
type OutboxStats func(ctx context.Context) (OutboxCounts, error)

const (
	outboxStatePending = "pending"
	outboxStateFailed  = "failed"
	outboxStatsTimeout = 2 * time.Second
)

type outboxCollector struct {
	stats   OutboxStats
	timeout time.Duration
	events  *prometheus.Desc
	oldest  *prometheus.Desc
	up      *prometheus.Desc
}

func newOutboxCollector(stats OutboxStats) *outboxCollector {
	return &outboxCollector{
		stats:   stats,
		timeout: outboxStatsTimeout,
		events:  prometheus.NewDesc(MetricOutboxEvents, "Outbox events that are not done, by state: pending has attempts left, failed ran out of them.", []string{"state"}, nil),
		oldest:  prometheus.NewDesc(MetricOutboxOldestPending, "Age of the oldest outbox event that is not done; 0 without one.", nil, nil),
		up:      prometheus.NewDesc(MetricOutboxStatsUp, "1 when the outbox counts of this scrape were read, 0 when the query failed.", nil, nil),
	}
}

func (c *outboxCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.events
	ch <- c.oldest
	ch <- c.up
}

func (c *outboxCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	counts, err := c.stats(ctx)
	if err != nil {
		log.Printf("metrics: read outbox counts: %v", err)
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.events, prometheus.GaugeValue, float64(counts.Pending), outboxStatePending)
	ch <- prometheus.MustNewConstMetric(c.events, prometheus.GaugeValue, float64(counts.Failed), outboxStateFailed)
	ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, counts.OldestPending.Seconds())
}
