package state

import "time"

// AppliedState is a group state the replication run applied: its reference
// as the satellite state lists it and the digest it applied.
type AppliedState struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
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
	f.mu.Lock()
	defer f.mu.Unlock()
	run := f.lastRun
	run.Applied = append([]AppliedState(nil), run.Applied...)
	return run
}

func (f *FetchAndReplicateStateProcess) recordRun(started time.Time, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run := RunSummary{
		StateDigest:  f.rootStateDigest,
		ConfigDigest: f.currentConfigDigest,
		Duration:     time.Since(started),
		FinishedAt:   time.Now(),
	}
	for _, group := range f.stateMap {
		if group.appliedDigest != "" {
			run.Applied = append(run.Applied, AppliedState{Ref: group.url, Digest: group.appliedDigest})
		}
	}
	if err != nil {
		run.LastReplicationError = err.Error()
	}
	f.lastRun = run
}
