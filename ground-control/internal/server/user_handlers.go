package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

const (
	roleAdmin       = "admin"
	roleSystemAdmin = "system_admin"
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
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	user, err := s.createUser(r.Context(), req.Username, req.Password)
	if err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	WriteJSONResponse(w, http.StatusCreated, newUserResponse(user))
}

// listUsersHandler lists all users except system_admin
func (s *Server) listUsersHandler(w http.ResponseWriter, r *http.Request) {
	users, err := s.listUsers(r.Context())
	if err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	response := make([]userResponse, 0, len(users))
	for _, user := range users {
		response = append(response, newUserResponse(user))
	}

	WriteJSONResponse(w, http.StatusOK, response)
}

// getUserHandler gets a specific user by username
func (s *Server) getUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	user, err := s.getUser(r.Context(), username)
	if err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	WriteJSONResponse(w, http.StatusOK, newUserResponse(user))
}

// deleteUserHandler deletes a user (system_admin only, cannot delete self)
func (s *Server) deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]

	currentUser, ok := GetUserFromContext(r.Context())
	if !ok {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := s.deleteUser(r.Context(), currentUser, username); err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// changeOwnPasswordHandler allows any authenticated user to change their password
func (s *Server) changeOwnPasswordHandler(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := GetUserFromContext(r.Context())
	if !ok {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.changeOwnPassword(r.Context(), currentUser, req.CurrentPassword, req.NewPassword); err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
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
		WriteJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.changeUserPassword(r.Context(), username, req.NewPassword); err != nil {
		statusCode, message := operationStatus(err)
		WriteJSONError(w, message, statusCode)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func newUserResponse(user userView) userResponse {
	return userResponse{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
	}
}
