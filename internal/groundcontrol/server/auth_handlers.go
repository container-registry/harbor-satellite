package server

import (
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

const maxFailedAttempts = 5

type refreshCredentialResponse struct {
	Secret string `json:"secret"`
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	if req.Username == "" || req.Password == "" {
		s.auditEvent(r, auditlog.AuditEvent{
			Operation:    auditlog.OpLogin,
			ResourceType: auditlog.ResSession,
			Outcome:      auditlog.OutcomeFailure,
			Actor:        req.Username,
			ActorType:    auditlog.ActorUser,
			Reason:       auditlog.ReasonMissingCredentials,
		})
		WriteJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	user, err := s.verifyUserCredentials(r.Context(), s.clientAddr(r), req.Username, req.Password)
	if err != nil {
		s.auditEvent(r, auditlog.AuditEvent{
			Operation:    auditlog.OpLogin,
			ResourceType: auditlog.ResSession,
			Outcome:      auditlog.OutcomeFailure,
			Actor:        req.Username,
			ActorType:    auditlog.ActorUser,
			Reason:       credentialFailureReason(err),
		})
		writeCredentialError(w, err)
		return
	}

	// Generate session token
	token, err := auth.GenerateSessionToken()
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(s.sessionDuration)
	_, err = s.dbQueries.CreateSession(r.Context(), database.CreateSessionParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpLogin,
		ResourceType: auditlog.ResSession,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        req.Username,
		ActorType:    auditlog.ActorUser,
	})

	WriteJSONResponse(w, http.StatusOK, LoginResponse{
		Token:     token,
		ExpiresAt: expiresAt,
	})
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := s.dbQueries.DeleteSession(r.Context(), token); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
