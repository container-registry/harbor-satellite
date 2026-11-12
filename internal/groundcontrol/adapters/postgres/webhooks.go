package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/webhook"
)

var (
	_ fleet.WebhookRepo         = (*WebhookRepo)(nil)
	_ webhook.SubscriptionStore = (*WebhookStore)(nil)
)

// WebhookRepo runs CreateWebhookSubscription, ListWebhookSubscriptions and
// DeleteWebhookSubscription (plan item 9.1).
type WebhookRepo struct{ q *database.Queries }

func NewWebhookRepo(db *sql.DB) (*WebhookRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the webhook repository needs a database", fleet.ErrInvalid)
	}
	return &WebhookRepo{q: database.New(db)}, nil
}

func kinds(types []string) []fleet.EventKind {
	out := make([]fleet.EventKind, 0, len(types))
	for _, t := range types {
		out = append(out, fleet.EventKind(t))
	}
	return out
}

func (r *WebhookRepo) Create(ctx context.Context, sub fleet.WebhookSubscription, sealedSecret []byte, createdBy string) (fleet.WebhookSubscription, error) {
	types := make([]string, 0, len(sub.EventTypes))
	for _, k := range sub.EventTypes {
		types = append(types, string(k))
	}
	row, err := r.q.CreateWebhookSubscription(ctx, database.CreateWebhookSubscriptionParams{
		Url: sub.URL, SecretCiphertext: sealedSecret, EventTypes: types,
		CreatedBy: sql.NullString{String: createdBy, Valid: createdBy != ""},
	})
	if err != nil {
		return fleet.WebhookSubscription{}, mapError(err)
	}
	return fleet.WebhookSubscription{
		ID: fleet.WebhookID(row.ID), URL: row.Url, EventTypes: kinds(row.EventTypes), Enabled: row.Enabled,
		DisabledReason: row.DisabledReason.String, CreatedAt: row.CreatedAt,
	}, nil
}

func (r *WebhookRepo) List(ctx context.Context) ([]fleet.WebhookSubscription, error) {
	rows, err := r.q.ListWebhookSubscriptions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]fleet.WebhookSubscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, fleet.WebhookSubscription{
			ID: fleet.WebhookID(row.ID), URL: row.Url, EventTypes: kinds(row.EventTypes), Enabled: row.Enabled,
			DisabledReason: row.DisabledReason.String, ConsecutiveFailures: row.Attempts, LastError: row.LastError.String,
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func (r *WebhookRepo) Delete(ctx context.Context, id fleet.WebhookID) error {
	n, err := r.q.DeleteWebhookSubscription(ctx, int32(id))
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return fmt.Errorf("webhook %d: %w", id, fleet.ErrNotFound)
	}
	return nil
}

// WebhookStore runs ListDueWebhooks, AdvanceWebhookCheckpoint and
// FailWebhookDelivery; xid8 values cross the driver as text.
type WebhookStore struct{ q *database.Queries }

func NewWebhookStore(db *sql.DB) (*WebhookStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: the webhook store needs a database", fleet.ErrInvalid)
	}
	return &WebhookStore{q: database.New(db)}, nil
}

func (s *WebhookStore) Due(ctx context.Context, limit int) ([]webhook.DueSubscription, error) {
	rows, err := s.q.ListDueWebhooks(ctx, rowLimit(limit))
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]webhook.DueSubscription, 0, len(rows))
	for _, r := range rows {
		xid, err := strconv.ParseUint(r.LastXid, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("webhook %d: checkpoint xid %q: %w", r.ID, r.LastXid, err)
		}
		out = append(out, webhook.DueSubscription{
			ID: fleet.WebhookID(r.ID), URL: r.Url, SealedSecret: r.SecretCiphertext, EventTypes: kinds(r.EventTypes),
			After: webhook.Position{XID: xid, ID: r.LastID}, Attempts: r.Attempts,
		})
	}
	return out, nil
}

func (s *WebhookStore) Advance(ctx context.Context, id fleet.WebhookID, to webhook.Position, status int) error {
	return mapError(s.q.AdvanceWebhookCheckpoint(ctx, database.AdvanceWebhookCheckpointParams{
		LastXid: strconv.FormatUint(to.XID, 10), LastID: to.ID, SubscriptionID: int32(id),
		LastStatus: sql.NullInt32{Int32: int32(status), Valid: status != 0},
	}))
}

func (s *WebhookStore) Fail(ctx context.Context, id fleet.WebhookID, status int, cause string, retryIn, disableAfter time.Duration) (bool, error) {
	row, err := s.q.FailWebhookDelivery(ctx, database.FailWebhookDeliveryParams{
		LastStatus: sql.NullInt32{Int32: int32(status), Valid: status != 0}, LastError: cause,
		RetryInMs: retryIn.Milliseconds(), SubscriptionID: int32(id), DisableAfterMs: disableAfter.Milliseconds(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapError(err)
	}
	return row.Disabled, nil
}
