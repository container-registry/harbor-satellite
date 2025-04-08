package replicator

import (
	"context"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/config"
	"github.com/container-registry/harbor-satellite/internal/transfer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Replicator struct {
	config *config.Config
	meter  *transfer.TransferMeter
	logger zerolog.Logger
}

func NewReplicator(cfg *config.Config, meter *transfer.TransferMeter) (*Replicator, error) {
	return &Replicator{
		config: cfg,
		meter:  meter,
		logger: log.With().Str("component", "replicator").Logger(),
	}, nil
}

func (r *Replicator) Replicate(ctx context.Context, source, destination string) error {
	if !r.meter.CheckQuota() {
		return fmt.Errorf("transfer quota exceeded")
	}

	r.meter.StartTransfer()

	err := r.doReplication(ctx, source, destination)
	if err != nil {
		r.meter.EndTransfer(0) // Transfer failed, count as 0 bytes
		return fmt.Errorf("replication failed: %w", err)
	}

	r.meter.EndTransfer(r.meter.GetCurrentTransferBytes())
	return nil
}

func (r *Replicator) doReplication(ctx context.Context, source, destination string) error {
	// TODO: Implement actual replication logic

	// For now, just simulate a transfer of 1MB
	r.meter.UpdateTransferBytes(1024 * 1024)

	return nil
}
