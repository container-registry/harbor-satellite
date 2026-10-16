package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.WebhookRepo = (*WebhookRepo)(nil)

// WebhookRepo is a recording fleet.WebhookRepo; its calls are recorded as
// "Webhooks.<Method>". Create returns the subscription with NextID.
type WebhookRepo struct {
	*Recorder
	Subscriptions []fleet.WebhookSubscription
	NextID        fleet.WebhookID
}

func NewWebhookRepo(rec *Recorder) *WebhookRepo {
	return &WebhookRepo{Recorder: orNew(rec)}
}

func (w *WebhookRepo) Create(_ context.Context, sub fleet.WebhookSubscription, sealedSecret []byte, createdBy string) (fleet.WebhookSubscription, error) {
	if err := w.Record("Webhooks.Create", sub, sealedSecret, createdBy); err != nil {
		return fleet.WebhookSubscription{}, err
	}
	sub.ID, sub.Enabled = w.NextID, true
	return sub, nil
}

func (w *WebhookRepo) List(context.Context) ([]fleet.WebhookSubscription, error) {
	if err := w.Record("Webhooks.List"); err != nil {
		return nil, err
	}
	return w.Subscriptions, nil
}

func (w *WebhookRepo) Delete(_ context.Context, id fleet.WebhookID) error {
	return w.Record("Webhooks.Delete", id)
}
