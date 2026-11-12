package fleet

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"time"
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
type WebhookService struct {
	c      *core
	repo   WebhookRepo
	sealer Sealer
}

// MinWebhookSecretBytes is the shortest HMAC secret a subscription takes.
const MinWebhookSecretBytes = 32

func webhookResource() Resource { return Resource{Type: ResourceWebhook} }

// Create fails with ErrConflict ("webhooks disabled") while
// Settings.WebhooksEnabled is off. It needs https unless the host is
// loopback, a secret of at least MinWebhookSecretBytes and event types from
// EventKinds(), else ErrInvalid. The secret is sealed with Deps.Sealer.
func (s *WebhookService) Create(ctx context.Context, sub WebhookSubscription, secret []byte) (WebhookSubscription, error) {
	if err := s.ready(); err != nil {
		return WebhookSubscription{}, err
	}
	if err := validateWebhook(sub, secret); err != nil {
		return WebhookSubscription{}, err
	}
	if err := s.c.authorize(ctx, ActionCreate, webhookResource()); err != nil {
		return WebhookSubscription{}, err
	}
	sealed, err := s.sealer.Seal(secret)
	if err != nil {
		return WebhookSubscription{}, fmt.Errorf("seal webhook secret: %w", err)
	}
	principal, _ := PrincipalFrom(ctx)
	return s.repo.Create(ctx, sub, sealed, principal.Subject)
}

func (s *WebhookService) List(ctx context.Context) ([]WebhookSubscription, error) {
	if s.repo == nil {
		return []WebhookSubscription{}, nil
	}
	if err := s.c.authorize(ctx, ActionList, webhookResource()); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *WebhookService) Delete(ctx context.Context, id WebhookID) error {
	if s.repo == nil {
		return fmt.Errorf("webhook %d: %w", id, ErrNotFound)
	}
	if err := s.c.authorize(ctx, ActionDelete, webhookResource()); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func (s *WebhookService) ready() error {
	if !s.c.settings.WebhooksEnabled {
		return fmt.Errorf("%w: webhooks disabled (WEBHOOKS_ENABLED)", ErrConflict)
	}
	if s.repo == nil || s.sealer == nil {
		return fmt.Errorf("%w: webhooks need a store and DATA_ENCRYPTION_KEY", ErrConflict)
	}
	return nil
}

func validateWebhook(sub WebhookSubscription, secret []byte) error {
	u, err := url.Parse(sub.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: webhook URL %q is not an absolute URL", ErrInvalid, sub.URL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		addr, perr := netip.ParseAddr(host)
		if host != "localhost" && (perr != nil || !addr.Unmap().IsLoopback()) {
			return fmt.Errorf("%w: webhook URL must be https unless the host is loopback", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: webhook URL scheme %q, want https", ErrInvalid, u.Scheme)
	}
	if len(secret) < MinWebhookSecretBytes {
		return fmt.Errorf("%w: webhook secret needs at least %d bytes", ErrInvalid, MinWebhookSecretBytes)
	}
	kinds := EventKinds()
	for _, k := range sub.EventTypes {
		if !slices.Contains(kinds, k) {
			return fmt.Errorf("%w: unknown event type %q", ErrInvalid, k)
		}
	}
	return nil
}
