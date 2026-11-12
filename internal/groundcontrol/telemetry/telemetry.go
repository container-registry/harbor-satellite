// Package telemetry serves Ground Control's Prometheus /metrics on a port
// separate from the API, as the satellite's telemetry package does.
package telemetry

import "time"

const (
	MetricHTTPRequests        = "groundcontrol_http_requests_total"
	MetricHTTPRequestDuration = "groundcontrol_http_request_duration_seconds"
	MetricHeartbeats          = "groundcontrol_heartbeats_total"
	MetricHeartbeatBuffer     = "groundcontrol_heartbeat_buffer_satellites"
	MetricHeartbeatFlushes    = "groundcontrol_heartbeat_flushes_total"
	MetricHeartbeatFlushTime  = "groundcontrol_heartbeat_flush_duration_seconds"
	MetricOutboxEvents        = "groundcontrol_outbox_events"
	MetricOutboxOldestPending = "groundcontrol_outbox_oldest_pending_age_seconds"
	MetricOutboxStatsUp       = "groundcontrol_outbox_stats_up"
)

const (
	OutcomeOK    = "ok"
	OutcomeError = "error"
	// RouteUnmatched labels requests no route matched (404, 405).
	RouteUnmatched = "unmatched"
)

// Recorder is how the HTTP server and the heartbeat path report what
// /metrics exposes.
type Recorder interface {
	// ObserveHTTP records one request by its route template, such as
	// /api/groups/{group}, so the label set stays bounded.
	ObserveHTTP(route, method string, code int, d time.Duration)
	CountHeartbeat()
	ObserveHeartbeatFlush(outcome string, d time.Duration)
}

// Nop discards every observation.
type Nop struct{}

var _ Recorder = Nop{}

func (Nop) ObserveHTTP(string, string, int, time.Duration) {}
func (Nop) CountHeartbeat()                                {}
func (Nop) ObserveHeartbeatFlush(string, time.Duration)    {}
