package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var (
	_ fleet.WebhookRepo         = (*WebhookRepo)(nil)
	_ webhook.SubscriptionStore = (*WebhookStore)(nil)
)

// WebhookRepo runs CreateWebhookSubscription, ListWebhookSubscriptions and
// DeleteWebhookSubscription (plan item 9.1).
type WebhookRepo struct{}

func NewWebhookRepo(db *sql.DB) (*WebhookRepo, error) {
	return nil, notimpl.Err("9.1 postgres webhook repository")
}

func (r *WebhookRepo) Create(ctx context.Context, sub fleet.WebhookSubscription, sealedSecret []byte, createdBy string) (fleet.WebhookSubscription, error) {
	return fleet.WebhookSubscription{}, notimpl.Err("9.1 postgres create webhook")
}

func (r *WebhookRepo) List(ctx context.Context) ([]fleet.WebhookSubscription, error) {
	return nil, notimpl.Err("9.1 postgres list webhooks")
}

func (r *WebhookRepo) Delete(ctx context.Context, id fleet.WebhookID) error {
	return notimpl.Err("9.1 postgres delete webhook")
}

// WebhookStore runs ListDueWebhooks, AdvanceWebhookCheckpoint and
// FailWebhookDelivery; xid8 values cross the driver as text.
type WebhookStore struct{}

func NewWebhookStore(db *sql.DB) (*WebhookStore, error) {
	return nil, notimpl.Err("9.1 postgres webhook store")
}

func (s *WebhookStore) Due(ctx context.Context, limit int) ([]webhook.DueSubscription, error) {
	return nil, notimpl.Err("9.1 postgres due webhooks")
}

func (s *WebhookStore) Advance(ctx context.Context, id fleet.WebhookID, to webhook.Position, status int) error {
	return notimpl.Err("9.1 postgres advance webhook checkpoint")
}

func (s *WebhookStore) Fail(ctx context.Context, id fleet.WebhookID, status int, cause string, retryIn, disableAfter time.Duration) (bool, error) {
	return false, notimpl.Err("9.1 postgres fail webhook delivery")
}
