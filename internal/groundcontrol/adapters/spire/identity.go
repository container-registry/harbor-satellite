// Package spire implements fleet.WorkloadIdentity over the SPIRE server
// API (plan item 1.3).
package spire

import (
	"context"
	"fmt"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
)

var _ fleet.WorkloadIdentity = (*WorkloadIdentity)(nil)

type WorkloadIdentity struct {
	client *spiffe.ServerClient
}

func New(client *spiffe.ServerClient) (*WorkloadIdentity, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: the SPIRE adapter needs a server client", fleet.ErrInvalid)
	}
	return &WorkloadIdentity{client: client}, nil
}

func (w *WorkloadIdentity) TrustDomain() string {
	return w.client.TrustDomainName()
}

func (w *WorkloadIdentity) CreateJoinToken(ctx context.Context, spiffeID string, ttl time.Duration) (string, error) {
	return w.client.CreateJoinToken(ctx, spiffeID, ttl)
}

func (w *WorkloadIdentity) CreateEntry(ctx context.Context, parentID, spiffeID string, selectors []string) (string, error) {
	return w.client.CreateWorkloadEntry(ctx, parentID, spiffeID, selectors)
}

func (w *WorkloadIdentity) DeleteEntry(ctx context.Context, entryID string) error {
	return w.client.DeleteWorkloadEntry(ctx, entryID)
}

func (w *WorkloadIdentity) DeleteEntriesFor(ctx context.Context, spiffeID string) error {
	return w.client.DeleteEntriesBySPIFFEID(ctx, spiffeID)
}

func (w *WorkloadIdentity) ListAgents(ctx context.Context, attestationType string) ([]fleet.Agent, error) {
	agents, err := w.client.ListAgents(ctx, attestationType)
	if err != nil {
		return nil, err
	}
	out := make([]fleet.Agent, 0, len(agents))
	for _, a := range agents {
		out = append(out, fleet.Agent{SPIFFEID: a.SpiffeID, AttestationType: a.AttestationType, Selectors: a.Selectors, ExpiresAt: a.ExpiresAt})
	}
	return out, nil
}
