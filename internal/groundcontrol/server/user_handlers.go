package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
)

// actorFromContext returns the authenticated user's username, or "unknown".
func actorFromContext(ctx context.Context) string {
	if u, ok := GetUserFromContext(ctx); ok {
		return u.Username
	}
	return "unknown"
}

const (
	roleAdmin       = "admin"
	roleSystemAdmin = "system_admin"
)

// CreateUser creates a new admin user (system_admin only)
func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		WriteJSONError(w, "Username is required", http.StatusBadRequest)
		return
	}

	if req.Username == "admin" {
		WriteJSONError(w, "Username 'admin' is reserved", http.StatusBadRequest)
		return
	}

	if err := s.passwordPolicy.Validate(req.Password); err != nil {
		WriteJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	user, err := s.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         roleAdmin,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			WriteJSONError(w, "User already exists", http.StatusConflict)
			return
		}
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpCreate,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actorFromContext(r.Context()),
		ActorType:    auditlog.ActorUser,
		Resource:     user.Username,
		Details:      map[string]any{"role": user.Role},
	})

	WriteJSONResponse(w, http.StatusCreated, UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	})
}

// ListUsers lists all users except system_admin
func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.dbQueries.ListUsers(r.Context())
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := make([]UserResponse, 0, len(users))
	for _, u := range users {
		response = append(response, UserResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		})
	}

	WriteJSONResponse(w, http.StatusOK, response)
}

// GetUser gets a specific user by username
func (s *Server) GetUser(w http.ResponseWriter, r *http.Request, username string) {
	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			WriteJSONError(w, "User not found", http.StatusNotFound)
			return
		}
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Hide system_admin from regular queries
	if user.Role == roleSystemAdmin {
		WriteJSONError(w, "User not found", http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	})
}

// DeleteUser deletes a user (system_admin only, cannot delete self)
func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request, username string) {
	currentUser, ok := GetUserFromContext(r.Context())
	if !ok {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if username == currentUser.Username {
		WriteJSONError(w, "Cannot delete yourself", http.StatusBadRequest)
		return
	}

	if username == "admin" {
		WriteJSONError(w, "Cannot delete system admin", http.StatusBadRequest)
		return
	}

	// Get user to delete their sessions
	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			WriteJSONError(w, "User not found", http.StatusNotFound)
			return
		}
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Delete user's sessions first (CASCADE should handle this, but explicit)
	if err := s.dbQueries.DeleteUserSessions(r.Context(), user.ID); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.dbQueries.DeleteUser(r.Context(), username); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpDelete,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        currentUser.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     username,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ChangeOwnPassword allows any authenticated user to change their password
func (s *Server) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := GetUserFromContext(r.Context())
	if !ok {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.passwordPolicy.Validate(req.NewPassword); err != nil {
		WriteJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Verify current password
	user, err := s.dbQueries.GetUserByUsername(r.Context(), currentUser.Username)
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	valid := auth.VerifyPassword(req.CurrentPassword, user.PasswordHash)
	if !valid {
		WriteJSONError(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.dbQueries.UpdateUserPassword(r.Context(), database.UpdateUserPasswordParams{
		Username:     currentUser.Username,
		PasswordHash: hash,
	}); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate all sessions including current session
	if err := s.dbQueries.DeleteUserSessions(r.Context(), user.ID); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpPasswordChange,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        currentUser.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     currentUser.Username,
		Details:      map[string]any{"flow": "self_service"},
	})

	w.WriteHeader(http.StatusNoContent)
}

// ChangeUserPassword allows system_admin to change any user's password
func (s *Server) ChangeUserPassword(w http.ResponseWriter, r *http.Request, username string) {
	var req ChangeUserPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.passwordPolicy.Validate(req.NewPassword); err != nil {
		WriteJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if user exists
	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			WriteJSONError(w, "User not found", http.StatusNotFound)
			return
		}
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.dbQueries.UpdateUserPassword(r.Context(), database.UpdateUserPasswordParams{
		Username:     username,
		PasswordHash: hash,
	}); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate all sessions for the user whose password was changed
	if err := s.dbQueries.DeleteUserSessions(r.Context(), user.ID); err != nil {
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.auditEvent(r, auditlog.AuditEvent{
		Operation:    auditlog.OpPasswordChange,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actorFromContext(r.Context()),
		ActorType:    auditlog.ActorUser,
		Resource:     username,
		Details:      map[string]any{"flow": "admin_reset"},
	})

	w.WriteHeader(http.StatusNoContent)
}
