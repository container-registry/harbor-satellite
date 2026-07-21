package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
)

const maxFailedAttempts = 5

// swagger:strfmt password
type swaggerPassword string

// swagger:strfmt date-time
type swaggerDateTime string

// LoginRequest contains user credentials for session creation.
//
// swagger:model LoginRequest
type loginRequest struct {
	// required: true
	Username string `json:"username"`
	// required: true
	Password swaggerPassword `json:"password"`
}

// LoginResponse contains a bearer token and its expiration timestamp.
//
// swagger:model LoginResponse
type loginResponse struct {
	Token     string          `json:"token"`
	ExpiresAt swaggerDateTime `json:"expires_at"`
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
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

	// Check if account is locked
	attempts, err := s.dbQueries.GetLoginAttempts(r.Context(), req.Username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		s.auditEvent(r, auditlog.AuditEvent{
			Operation:    auditlog.OpLogin,
			ResourceType: auditlog.ResSession,
			Outcome:      auditlog.OutcomeFailure,
			Actor:        req.Username,
			ActorType:    auditlog.ActorUser,
			Reason:       auditlog.ReasonAccountLocked,
		})
		WriteJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Get user
	user, err := s.dbQueries.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		s.recordFailedAttempt(r, req.Username)
		s.auditEvent(r, auditlog.AuditEvent{
			Operation:    auditlog.OpLogin,
			ResourceType: auditlog.ResSession,
			Outcome:      auditlog.OutcomeFailure,
			Actor:        req.Username,
			ActorType:    auditlog.ActorUser,
			Reason:       auditlog.ReasonUnknownUser,
		})
		WriteJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Verify password
	valid := auth.VerifyPassword(string(req.Password), user.PasswordHash)
	if !valid {
		s.recordFailedAttempt(r, req.Username)
		s.auditEvent(r, auditlog.AuditEvent{
			Operation:    auditlog.OpLogin,
			ResourceType: auditlog.ResSession,
			Outcome:      auditlog.OutcomeFailure,
			Actor:        req.Username,
			ActorType:    auditlog.ActorUser,
			Reason:       auditlog.ReasonBadPassword,
		})
		WriteJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	_ = s.dbQueries.ResetLoginAttempts(r.Context(), req.Username) //nolint:errcheck // Reset failed attempts on success (ignore errors)

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

	WriteJSONResponse(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresAt: swaggerDateTime(expiresAt.Format(time.RFC3339)),
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

func (s *Server) recordFailedAttempt(r *http.Request, username string) {
	attempts, err := s.dbQueries.UpsertLoginAttempt(r.Context(), username)
	if err != nil {
		return
	}

	if attempts.FailedCount >= maxFailedAttempts {
		lockUntil := time.Now().Add(s.lockoutDuration)
		if err := s.dbQueries.LockAccount(r.Context(), database.LockAccountParams{
			Username:    username,
			LockedUntil: sql.NullTime{Time: lockUntil, Valid: true},
		}); err != nil {
			log.Printf("Failed to lock account %s: %v", username, err)
		}
	}
}
