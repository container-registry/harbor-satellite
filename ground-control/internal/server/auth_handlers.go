package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

const (
	maxFailedAttempts = 5
	lockoutDuration   = 15 * time.Minute
	sessionDuration   = 24 * time.Hour
)

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
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Check if account is locked
	attempts, err := s.dbQueries.GetLoginAttempts(r.Context(), req.Username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Get user
	user, err := s.dbQueries.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		s.recordFailedAttempt(r, req.Username)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Verify password
	valid, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !valid {
		s.recordFailedAttempt(r, req.Username)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Reset failed attempts on success (ignore errors)
	_ = s.dbQueries.ResetLoginAttempts(r.Context(), req.Username)

	// Generate session token
	token, err := auth.GenerateSessionToken()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(sessionDuration)
	_, err = s.dbQueries.CreateSession(r.Context(), database.CreateSessionParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	WriteJSONResponse(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	})
}

func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := s.dbQueries.DeleteSession(r.Context(), token); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
		lockUntil := time.Now().Add(lockoutDuration)
		if err := s.dbQueries.LockAccount(r.Context(), database.LockAccountParams{
			Username:    username,
			LockedUntil: sql.NullTime{Time: lockUntil, Valid: true},
		}); err != nil {
			log.Printf("Failed to lock account %s: %v", username, err)
		}
	}
}
