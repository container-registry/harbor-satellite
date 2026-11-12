package server

import (
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/spiffe"
)

// RefreshSatelliteCredentials serves POST /satellites/refresh. The
// satellite auth chain has already authenticated the caller by SVID or
// robot Basic auth and put its name in the context.
func (s *Server) RefreshSatelliteCredentials(w http.ResponseWriter, r *http.Request) {
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
