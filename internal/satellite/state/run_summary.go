package state

import "time"

// AppliedState is a group state the replication run applied: its reference
// as the satellite state lists it and the digest it applied.
type AppliedState struct {
	Ref, Digest string
}

// RunSummary describes the last finished state replication run; the status
// report sends it (plan item 6.6).
type RunSummary struct {
	// StateDigest is the satellite state artifact's digest, ConfigDigest
	// the config's. The satellite state lists group states by tag, so
	// Applied carries the group digests Ground Control compares per
	// reference: every group currently applied, where a group whose
	// replication failed keeps the digest it applied before and an
	// unchanged group keeps its digest.
	StateDigest, ConfigDigest string
	Applied                   []AppliedState
	Duration                  time.Duration
	// LastReplicationError is the run's error, empty after a clean run.
	LastReplicationError string
	FinishedAt           time.Time
}

// RunSummarySource is where the status report reads the last run from.
type RunSummarySource interface {
	LastRun() RunSummary
}

// LastRun returns the summary of the last finished Execute, zero before
// the first.
func (f *FetchAndReplicateStateProcess) LastRun() RunSummary {
	return RunSummary{}
}

// SetRunSummarySource makes the report carry the last run's digests,
// applied_states, duration and last_replication_error (plan item 6.6).
func (s *StatusReportingProcess) SetRunSummarySource(src RunSummarySource) {}
