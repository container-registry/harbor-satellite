package state

import (
	"context"

	"github.com/container-registry/harbor-satellite/pkg/config"
)

// PeerAwareReplicator wraps an existing Replicator and attempts peer acquisition before fallback.
// In this initial architectural milestone, it strictly delegates all entities to the fallback replicator.
type PeerAwareReplicator struct {
	p2pCfg   config.P2PConfig
	fallback Replicator

	localURL      string
	localUsername string
	localPassword string
	useUnsecure   bool
}

// NewPeerAwareReplicator creates a new PeerAwareReplicator that delegates to the provided fallback.
// Panics if the fallback replicator is nil.
func NewPeerAwareReplicator(
	cfg config.P2PConfig,
	fallback Replicator,
	localURL string,
	localUsername string,
	localPassword string,
	useUnsecure bool,
) *PeerAwareReplicator {
	if fallback == nil {
		panic("fallback replicator cannot be nil")
	}

	return &PeerAwareReplicator{
		p2pCfg:        cfg,
		fallback:      fallback,
		localURL:      localURL,
		localUsername: localUsername,
		localPassword: localPassword,
		useUnsecure:   useUnsecure,
	}
}

// Replicate delegates to the wrapped fallback replicator. Peer-to-peer acquisition logic will be added in a future PR.
func (p *PeerAwareReplicator) Replicate(ctx context.Context, replicationEntities []Entity) error {
	return p.fallback.Replicate(ctx, replicationEntities)
}

// DeleteReplicationEntity delegates directly to the fallback replicator.
func (p *PeerAwareReplicator) DeleteReplicationEntity(ctx context.Context, replicationEntities []Entity) error {
	return p.fallback.DeleteReplicationEntity(ctx, replicationEntities)
}
