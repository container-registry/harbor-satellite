package server

import (
	"encoding/json"
	"net/http"
	"time"
)

const maxFailedAttempts = 5

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := s.login(r.Context(), req.Username, req.Password)
	if err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	WriteJSONResponse(w, http.StatusOK, loginResponse{
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.logout(r.Context(), extractToken(r)); err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
