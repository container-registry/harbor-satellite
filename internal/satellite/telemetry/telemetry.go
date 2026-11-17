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
	MetricHeartbeats          = "satellite_heartbeats_total"
	MetricJournalAcks         = "satellite_journal_acks_total"
	MetricJournalAckedSeq     = "satellite_journal_acked_seq"
	MetricHeartbeatsTooLarge  = "satellite_heartbeats_too_large_total"
)

// Heartbeat codes of CountHeartbeat besides an HTTP status.
const HeartbeatTransportError = "error"

// Reactions of CountHeartbeatTooLarge to a 413 from Ground Control.
const (
	// TooLargeHalvedBatch: the next beats upload half as many journal records.
	TooLargeHalvedBatch = "halved_batch"
	// TooLargeWithoutJournal: the next beat carries neither the journal nor
	// the cached-image inventory.
	TooLargeWithoutJournal = "without_journal"
	// TooLargeNone: the beat already carried neither; nothing is left to cut.
	TooLargeNone = "none"
)

// Journal ack outcomes of ObserveJournalAck.
const (
	JournalAccepted = "accepted"
	JournalRejected = "rejected"
	JournalDropped  = "dropped"
)

// Recorder is how the proxy, replication and store report what /metrics exposes.
type Recorder interface {
	ObservePull(resource string, outcome string, d time.Duration)
	CountPullThroughFill(outcome string)
	ObserveReplication(bytes int64, d time.Duration, outcome string)
	ObserveStoreLockWait(operation string, d time.Duration)
	// CountHeartbeat counts one heartbeat by Ground Control's HTTP status,
	// or HeartbeatTransportError when no answer arrived.
	CountHeartbeat(code string)
	// ObserveJournalAck counts Ground Control's answer to a journal upload
	// and records the seq it holds.
	ObserveJournalAck(outcome string, ackedSeq int64)
	// CountHeartbeatTooLarge counts one heartbeat Ground Control refused
	// with 413, by the satellite's reaction.
	CountHeartbeatTooLarge(reaction string)
}

// Nop discards every observation.
type Nop struct{}

var _ Recorder = Nop{}

func (Nop) ObservePull(string, string, time.Duration)       {}
func (Nop) CountPullThroughFill(string)                     {}
func (Nop) ObserveReplication(int64, time.Duration, string) {}
func (Nop) ObserveStoreLockWait(string, time.Duration)      {}
func (Nop) CountHeartbeat(string)                           {}
func (Nop) ObserveJournalAck(string, int64)                 {}
func (Nop) CountHeartbeatTooLarge(string)                   {}
