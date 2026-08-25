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
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
)

const maxFailedAttempts = 5

type refreshCredentialResponse struct {
	Secret string `json:"secret"`
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
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
	valid := auth.VerifyPassword(req.Password, user.PasswordHash)
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

func (s *Server) RefreshSatellite(w http.ResponseWriter, r *http.Request) {
	// Check SPIFFE identity first for dual auth
	var satelliteName string
	if name, ok := spiffe.GetSatelliteName(r.Context()); ok {
		satelliteName = name
	} else {
		HandleAppError(w, &AppError{
			Message: "unknown satellite entity",
			Code:    http.StatusForbidden,
		})
		return
	}

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		log.Printf("Unknown satellite: %s", satelliteName)
		HandleAppError(w, &AppError{
			Message: "unknown satellite entity",
			Code:    http.StatusForbidden,
		})
		return
	}

	robotAcc, err := s.dbQueries.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Failed to find robot account for satellite : %s", satelliteName)
		HandleAppError(w, &AppError{
			Message: "Failed to find robot account",
			Code:    http.StatusForbidden,
		})
		return
	}

	newSecret, err := refreshRobotSecret(r, s.dbQueries, robotAcc)
	if err != nil {
		log.Printf("Failed to refresh robot secret for satellite %s: %v", satelliteName, err)
		HandleAppError(w, &AppError{
			Message: "Error: failed to refresh robot secret",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	WriteJSONResponse(w, http.StatusOK, refreshCredentialResponse{
		Secret: newSecret,
	})
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
