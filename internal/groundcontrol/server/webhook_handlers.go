package server

import (
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// The webhook API (plan item 9.1) decodes, calls fleet.WebhookService and
// encodes; system admins only (requiresSystemAdmin).

func webhookError(w http.ResponseWriter, op string, err error) {
	if StatusForError(err) == http.StatusInternalServerError {
		log.Printf("webhook %s: %v", op, err)
	}
	handleFleetError(w, err, nil)
}

func toWebhook(sub fleet.WebhookSubscription) WebhookSubscription {
	types := make([]string, 0, len(sub.EventTypes))
	for _, k := range sub.EventTypes {
		types = append(types, string(k))
	}
	return WebhookSubscription{
		ID: int32(sub.ID), URL: sub.URL, EventTypes: types, Enabled: sub.Enabled, DisabledReason: sub.DisabledReason,
		ConsecutiveFailures: sub.ConsecutiveFailures, LastError: sub.LastError, CreatedAt: sub.CreatedAt,
	}
}

func (s *Server) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	subs, err := s.svc().Webhooks.List(r.Context())
	if err != nil {
		webhookError(w, "list", err)
		return
	}
	out := make([]WebhookSubscription, 0, len(subs))
	for _, sub := range subs {
		out = append(out, toWebhook(sub))
	}
	WriteJSONResponse(w, http.StatusOK, out)
}

func (s *Server) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req WebhookSubscriptionRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	kinds := make([]fleet.EventKind, 0, len(req.EventTypes))
	for _, k := range req.EventTypes {
		kinds = append(kinds, fleet.EventKind(k))
	}
	sub, err := s.svc().Webhooks.Create(r.Context(), fleet.WebhookSubscription{URL: req.URL, EventTypes: kinds}, []byte(req.Secret))
	if err != nil {
		webhookError(w, "create", err)
		return
	}
	WriteJSONResponse(w, http.StatusCreated, toWebhook(sub))
}

func (s *Server) DeleteWebhook(w http.ResponseWriter, r *http.Request, id int32) {
	if err := s.svc().Webhooks.Delete(r.Context(), fleet.WebhookID(id)); err != nil {
		webhookError(w, "delete", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
