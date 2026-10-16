package server

import "net/http"

func (s *Server) ListWebhooks(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "9.1 list webhooks")
}

func (s *Server) CreateWebhook(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "9.1 create webhook")
}

func (s *Server) DeleteWebhook(w http.ResponseWriter, _ *http.Request, _ int32) {
	notImplemented(w, "9.1 delete webhook")
}
