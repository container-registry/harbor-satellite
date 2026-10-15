// Package telemetry serves the satellite's /healthz and /metrics on a local
// port separate from the OCI proxy (plan item 0.4).
package telemetry

import "time"

const (
	MetricPullDuration        = "satellite_proxy_pull_duration_seconds"
	MetricPullThroughFills    = "satellite_proxy_pull_through_fills_total"
	MetricReplicationBytes    = "satellite_replication_bytes_total"
	MetricReplicationDuration = "satellite_replication_duration_seconds"
	MetricStoreLockWait       = "satellite_store_lock_wait_seconds"
)

// Recorder is how the proxy, replication and store report what /metrics exposes.
type Recorder interface {
	ObservePull(resource string, outcome string, d time.Duration)
	CountPullThroughFill(outcome string)
	ObserveReplication(bytes int64, d time.Duration, outcome string)
	ObserveStoreLockWait(operation string, d time.Duration)
}

// Nop discards every observation.
type Nop struct{}

var _ Recorder = Nop{}

func (Nop) ObservePull(string, string, time.Duration)       {}
func (Nop) CountPullThroughFill(string)                     {}
func (Nop) ObserveReplication(int64, time.Duration, string) {}
func (Nop) ObserveStoreLockWait(string, time.Duration)      {}
