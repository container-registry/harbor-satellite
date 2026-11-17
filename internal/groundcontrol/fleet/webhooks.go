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
// loopback, no BlockedWebhookAddr literal as host, a secret of at least MinWebhookSecretBytes and event types from
// EventKinds(), else ErrInvalid. The secret is sealed with Deps.Sealer.
func (s *WebhookService) Create(ctx context.Context, sub WebhookSubscription, secret []byte) (WebhookSubscription, error) {
	if err := s.c.authorize(ctx, ActionCreate, webhookResource()); err != nil {
		return WebhookSubscription{}, err
	}
	if err := s.ready(); err != nil {
		return WebhookSubscription{}, err
	}
	if err := validateWebhook(sub, secret); err != nil {
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
	if err := s.c.authorize(ctx, ActionList, webhookResource()); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []WebhookSubscription{}, nil
	}
	return s.repo.List(ctx)
}

func (s *WebhookService) Delete(ctx context.Context, id WebhookID) error {
	if err := s.c.authorize(ctx, ActionDelete, webhookResource()); err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("webhook %d: %w", id, ErrNotFound)
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

// BlockedWebhookAddr reports the addresses no webhook is delivered to:
// link-local, which covers the cloud metadata endpoints (ADR-0019), and
// unspecified. Create refuses them as IP literals, the sender at dial time.
func BlockedWebhookAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
}

func validateWebhook(sub WebhookSubscription, secret []byte) error {
	u, err := url.Parse(sub.URL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("%w: webhook URL %q is not an absolute URL with a host", ErrInvalid, sub.URL)
	}
	host := u.Hostname()
	if addr, perr := netip.ParseAddr(host); perr == nil && BlockedWebhookAddr(addr) {
		return fmt.Errorf("%w: webhook URL host %s is link-local or unspecified", ErrInvalid, host)
	}
	switch u.Scheme {
	case "https":
	case "http":
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
