package fleettest

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.WorkloadIdentity = (*WorkloadIdentity)(nil)

// WorkloadIdentity is a recording fleet.WorkloadIdentity.
type WorkloadIdentity struct {
	*Recorder
	// Domain is what TrustDomain returns.
	Domain string
	// JoinToken and EntryID are what CreateJoinToken and CreateEntry return.
	JoinToken, EntryID string
	// Agents is what ListAgents returns.
	Agents []fleet.Agent
}

// NewWorkloadIdentity records into rec, or into a new Recorder when rec is
// nil.
func NewWorkloadIdentity(rec *Recorder) *WorkloadIdentity {
	return &WorkloadIdentity{Recorder: orNew(rec)}
}

// TrustDomain is not recorded: it reads configuration.
func (w *WorkloadIdentity) TrustDomain() string {
	return w.Domain
}

func (w *WorkloadIdentity) CreateJoinToken(_ context.Context, spiffeID string, ttl time.Duration) (string, error) {
	if err := w.Record("CreateJoinToken", spiffeID, ttl); err != nil {
		return "", err
	}
	return w.JoinToken, nil
}

func (w *WorkloadIdentity) CreateEntry(_ context.Context, parentID, spiffeID string, selectors []string) (string, error) {
	if err := w.Record("CreateEntry", parentID, spiffeID, selectors); err != nil {
		return "", err
	}
	return w.EntryID, nil
}

func (w *WorkloadIdentity) DeleteEntry(_ context.Context, entryID string) error {
	return w.Record("DeleteEntry", entryID)
}

func (w *WorkloadIdentity) DeleteEntriesFor(_ context.Context, spiffeID string) error {
	return w.Record("DeleteEntriesFor", spiffeID)
}

func (w *WorkloadIdentity) ListAgents(_ context.Context, attestationType string) ([]fleet.Agent, error) {
	if err := w.Record("ListAgents", attestationType); err != nil {
		return nil, err
	}
	return w.Agents, nil
}
