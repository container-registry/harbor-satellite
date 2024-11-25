package notifier

import (
	"context"

	"container-registry.com/harbor-satellite/logger"
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
