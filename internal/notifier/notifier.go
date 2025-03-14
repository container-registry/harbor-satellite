package notifier

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/logger"
)

type Notifier interface {
	// Notify sends a notification
	Notify() error
}

type SimpleNotifier struct{
	ctx context.Context
}

func NewSimpleNotifier(ctx context.Context) Notifier {
	return &SimpleNotifier{
		ctx: ctx,
	}
}

func (n *SimpleNotifier) Notify() error {
	log := logger.FromContext(n.ctx)
	log.Info().Msg("This is a simple notifier")
	return nil
}
