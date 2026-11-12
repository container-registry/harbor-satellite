// Package telemetrytest records telemetry observations for assertions.
package telemetrytest

import (
	"sync"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
)

type Pull struct {
	Resource, Outcome string
}

type Replication struct {
	Bytes   int64
	Outcome string
}

// Spy is a telemetry.Recorder that keeps every observation.
type Spy struct {
	mu           sync.Mutex
	Pulls        []Pull
	Fills        []string
	Replications []Replication
	LockWaits    []string
	Heartbeats   []string
	JournalAcks  []JournalAck
	TooLarge     []string
}

type JournalAck struct {
	Outcome  string
	AckedSeq int64
}

var _ telemetry.Recorder = (*Spy)(nil)

func (s *Spy) ObservePull(resource, outcome string, _ time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Pulls = append(s.Pulls, Pull{Resource: resource, Outcome: outcome})
}

func (s *Spy) CountPullThroughFill(outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Fills = append(s.Fills, outcome)
}

func (s *Spy) ObserveReplication(bytes int64, _ time.Duration, outcome string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Replications = append(s.Replications, Replication{Bytes: bytes, Outcome: outcome})
}

func (s *Spy) ObserveStoreLockWait(operation string, _ time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LockWaits = append(s.LockWaits, operation)
}

func (s *Spy) CountHeartbeat(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Heartbeats = append(s.Heartbeats, code)
}

func (s *Spy) ObserveJournalAck(outcome string, ackedSeq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.JournalAcks = append(s.JournalAcks, JournalAck{Outcome: outcome, AckedSeq: ackedSeq})
}

func (s *Spy) CountHeartbeatTooLarge(reaction string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TooLarge = append(s.TooLarge, reaction)
}

// Snapshot copies the observations under the lock.
func (s *Spy) Snapshot() Spy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Spy{
		Pulls:        append([]Pull(nil), s.Pulls...),
		Fills:        append([]string(nil), s.Fills...),
		Replications: append([]Replication(nil), s.Replications...),
		LockWaits:    append([]string(nil), s.LockWaits...),
		Heartbeats:   append([]string(nil), s.Heartbeats...),
		JournalAcks:  append([]JournalAck(nil), s.JournalAcks...),
		TooLarge:     append([]string(nil), s.TooLarge...),
	}
}
