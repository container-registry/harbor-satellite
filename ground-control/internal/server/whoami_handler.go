package server

import (
	"net/http"
)

// whoamiResponse is the JSON response for GET /api/whoami.
type whoamiResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// whoamiHandler returns the identity of the currently authenticated user.
func (s *Server) whoamiHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := GetUserFromContext(r.Context())
	if !ok {
		WriteJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	WriteJSONResponse(w, http.StatusOK, whoamiResponse{
		Username: user.Username,
		Role:     user.Role,
	})
}