package replicate

import (
	"context"
)

type Replicator interface {
	// Replicate copies images from the source registry to the local registry.
	Replicate(ctx context.Context, ref string) error
}

type BasicReplicator struct{}

func NewReplicator() Replicator {
	return &BasicReplicator{}
}

func (r *BasicReplicator) Replicate(ctx context.Context, ref string) error {
	// Placeholder for replication logic
	return nil
}
