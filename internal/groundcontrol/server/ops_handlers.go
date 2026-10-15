package server

import "net/http"

// ListOutboxEvents lists the outbox events that are not done, so operators
// see what the async plane still has to do and what keeps failing.
func (s *Server) ListOutboxEvents(w http.ResponseWriter, _ *http.Request, _ ListOutboxEventsParams) {
	notImplemented(w, "2.3 ops outbox")
}
