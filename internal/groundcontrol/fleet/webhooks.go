package fleet

import (
	"context"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type WebhookID int32

type WebhookSubscription struct {
	ID                  WebhookID
	URL                 string
	EventTypes          []EventKind // empty means every kind
	Enabled             bool
	DisabledReason      string
	ConsecutiveFailures int32
	LastError           string
	CreatedAt           time.Time
}

// WebhookRepo stores subscriptions; Create starts the checkpoint at the
// current xmin, so events committed before are not delivered (plan item
// 9.1). A disabled subscription is re-enabled by deleting and creating it.
type WebhookRepo interface {
	Create(ctx context.Context, sub WebhookSubscription, sealedSecret []byte, createdBy string) (WebhookSubscription, error)
	List(ctx context.Context) ([]WebhookSubscription, error)
	Delete(ctx context.Context, id WebhookID) error
}

// WebhookService manages webhook subscriptions (plan item 9.1, ADR-0019).
type WebhookService struct{}

// MinWebhookSecretBytes is the shortest HMAC secret a subscription takes.
const MinWebhookSecretBytes = 32

// Create fails with ErrConflict ("webhooks disabled") while
// Settings.WebhooksEnabled is off. It needs https unless the host is
// loopback, a secret of at least MinWebhookSecretBytes and event types from
// EventKinds(), else ErrInvalid. The secret is sealed with Deps.Sealer.
func (s *WebhookService) Create(ctx context.Context, sub WebhookSubscription, secret []byte) (WebhookSubscription, error) {
	return WebhookSubscription{}, notimpl.Err("9.1 create webhook")
}

func (s *WebhookService) List(ctx context.Context) ([]WebhookSubscription, error) {
	return nil, notimpl.Err("9.1 list webhooks")
}

func (s *WebhookService) Delete(ctx context.Context, id WebhookID) error {
	return notimpl.Err("9.1 delete webhook")
}
