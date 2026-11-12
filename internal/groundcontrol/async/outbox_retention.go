package async

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// OutboxPruner deletes done outbox events older than OUTBOX_RETENTION
// (plan item 9.1). It is the only retention of the journal's log-only edge
// events, so it runs whether or not anyone subscribes, and it never passes
// the checkpoint of an enabled webhook subscription.
type OutboxPruner struct {
	store               OutboxStore
	retention, interval time.Duration
	batch               int
}

var _ Job = (*OutboxPruner)(nil)

func NewOutboxPruner(store OutboxStore, retention, interval time.Duration, batch int) (*OutboxPruner, error) {
	if store == nil || retention <= 0 || interval <= 0 || batch <= 0 {
		return nil, fmt.Errorf("%w: the outbox pruner needs a store, a positive retention, interval and batch", fleet.ErrInvalid)
	}
	return &OutboxPruner{store: store, retention: retention, interval: interval, batch: batch}, nil
}

func (p *OutboxPruner) Name() string { return "outbox-pruner" }

// Run calls RunOnce every interval until ctx ends.
func (p *OutboxPruner) Run(ctx context.Context) error {
	every(ctx, p.interval, func(ctx context.Context) {
		if _, err := p.RunOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("outbox pruner: %v", err)
		}
	})
	return nil
}

// RunOnce prunes batch by batch until a batch deletes fewer than batch rows.
func (p *OutboxPruner) RunOnce(ctx context.Context) (int64, error) {
	var total int64
	for {
		n, err := p.store.Prune(ctx, p.retention, p.batch)
		total += n
		if err != nil {
			return total, fmt.Errorf("prune outbox: %w", err)
		}
		if n < int64(p.batch) {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}
