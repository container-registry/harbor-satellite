// Package spire implements fleet.WorkloadIdentity over the SPIRE server
// API (plan item 1.3).
package spire

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ fleet.WorkloadIdentity = (*WorkloadIdentity)(nil)

type WorkloadIdentity struct{}

func New(client *spiffe.ServerClient) (*WorkloadIdentity, error) {
	return nil, notimpl.Err("1.3 SPIRE workload identity")
}

func (w *WorkloadIdentity) TrustDomain() string {
	return ""
}

func (w *WorkloadIdentity) CreateJoinToken(ctx context.Context, spiffeID string, ttl time.Duration) (string, error) {
	return "", notimpl.Err("1.3 SPIRE join token")
}

func (w *WorkloadIdentity) CreateEntry(ctx context.Context, parentID, spiffeID string, selectors []string) (string, error) {
	return "", notimpl.Err("1.3 SPIRE create entry")
}

func (w *WorkloadIdentity) DeleteEntry(ctx context.Context, entryID string) error {
	return notimpl.Err("1.3 SPIRE delete entry")
}

func (w *WorkloadIdentity) DeleteEntriesFor(ctx context.Context, spiffeID string) error {
	return notimpl.Err("1.3 SPIRE delete entries")
}

func (w *WorkloadIdentity) ListAgents(ctx context.Context, attestationType string) ([]fleet.Agent, error) {
	return nil, notimpl.Err("1.3 SPIRE list agents")
}
