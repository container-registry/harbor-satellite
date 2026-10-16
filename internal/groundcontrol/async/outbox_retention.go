package async

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// OutboxPruner deletes done outbox events older than OUTBOX_RETENTION
// (plan item 9.1). It is the only retention of the journal's log-only edge
// events, so it runs whether or not anyone subscribes, and it never passes
// the checkpoint of an enabled webhook subscription.
type OutboxPruner struct{}

var _ Job = (*OutboxPruner)(nil)

func NewOutboxPruner(store OutboxStore, retention, interval time.Duration, batch int) (*OutboxPruner, error) {
	return nil, notimpl.Err("9.1 outbox pruner")
}

func (p *OutboxPruner) Name() string { return "outbox-pruner" }

// Run calls RunOnce every interval until ctx ends.
func (p *OutboxPruner) Run(ctx context.Context) error {
	return notimpl.Err("9.1 outbox pruner run")
}

// RunOnce prunes batch by batch until a batch deletes fewer than batch rows.
func (p *OutboxPruner) RunOnce(ctx context.Context) (int64, error) {
	return 0, notimpl.Err("9.1 outbox pruner run once")
}
