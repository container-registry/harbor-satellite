package server

import (
	"log"
	"net/http"
)

// ListOutboxEvents serves GET /api/ops/outbox: the events that are not
// done, or with state=failed only those that ran out of attempts.
func (s *Server) ListOutboxEvents(w http.ResponseWriter, r *http.Request, params ListOutboxEventsParams) {
	if s.outbox == nil {
		HandleAppError(w, &AppError{Message: "the outbox is not available", Code: http.StatusServiceUnavailable})
		return
	}
	var failedOnly bool
	switch params.State {
	case "", "pending":
	case "failed":
		failedOnly = true
	default:
		HandleAppError(w, &AppError{Message: "state must be pending or failed", Code: http.StatusBadRequest})
		return
	}
	limit := int(params.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	events, err := s.outbox.ListOutbox(r.Context(), failedOnly, limit)
	if err != nil {
		log.Printf("list outbox events: %v", err)
		HandleAppError(w, &AppError{Message: "Error: failed to list outbox events", Code: http.StatusInternalServerError})
		return
	}
	if events == nil {
		events = []OutboxEventResponse{}
	}
	WriteJSONResponse(w, http.StatusOK, events)
}
