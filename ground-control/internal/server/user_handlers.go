package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/lib/pq"

	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
)

const (
	minPasswordLength = 8
	roleAdmin         = "admin"
	roleSystemAdmin   = "system_admin"
)

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        int32  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type changeUserPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// createUserHandler creates a new admin user (system_admin only)
func (s *Server) createUserHandler(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	if req.Username == "admin" {
		http.Error(w, "Username 'admin' is reserved", http.StatusBadRequest)
		return
	}

	if len(req.Password) < minPasswordLength {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
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
			http.Error(w, "User already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(userResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// listUsersHandler lists all users except system_admin
func (s *Server) listUsersHandler(w http.ResponseWriter, r *http.Request) {
	users, err := s.dbQueries.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := make([]userResponse, 0, len(users))
	for _, u := range users {
		response = append(response, userResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// getUserHandler gets a specific user by username
func (s *Server) getUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Hide system_admin from regular queries
	if user.Role == roleSystemAdmin {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(userResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// deleteUserHandler deletes a user (system_admin only, cannot delete self)
func (s *Server) deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	currentUser, ok := GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if username == currentUser.Username {
		http.Error(w, "Cannot delete yourself", http.StatusBadRequest)
		return
	}

	if username == "admin" {
		http.Error(w, "Cannot delete system admin", http.StatusBadRequest)
		return
	}

	// Get user to delete their sessions
	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Delete user's sessions first (CASCADE should handle this, but explicit)
	if err := s.dbQueries.DeleteUserSessions(r.Context(), user.ID); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.dbQueries.DeleteUser(r.Context(), username); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// changeOwnPasswordHandler allows any authenticated user to change their password
func (s *Server) changeOwnPasswordHandler(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < minPasswordLength {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// Verify current password
	user, err := s.dbQueries.GetUserByUsername(r.Context(), currentUser.Username)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	valid, err := auth.VerifyPassword(req.CurrentPassword, user.PasswordHash)
	if err != nil || !valid {
		http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.dbQueries.UpdateUserPassword(r.Context(), database.UpdateUserPasswordParams{
		Username:     currentUser.Username,
		PasswordHash: hash,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// changeUserPasswordHandler allows system_admin to change any user's password
func (s *Server) changeUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	var req changeUserPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < minPasswordLength {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// Check if user exists
	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.dbQueries.UpdateUserPassword(r.Context(), database.UpdateUserPasswordParams{
		Username:     username,
		PasswordHash: hash,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate all sessions for the user whose password was changed
	if err := s.dbQueries.DeleteUserSessions(r.Context(), user.ID); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
