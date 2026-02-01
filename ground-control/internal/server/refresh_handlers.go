package server

import (
	"log"
	"net/http"
	"strconv"

	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/gorilla/mux"
)

type RefreshCredentialsResponse struct {
	RobotName string `json:"robot_name"`
	Secret    string `json:"secret"`
}

func (s *Server) refreshCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satelliteName := vars["satellite"]

	// No transaction needed as we are only reading from DB and updating Harbor/Config
	q := s.dbQueries

	sat, err := q.GetSatelliteByName(r.Context(), satelliteName)
	if err != nil {
		HandleAppError(w, &AppError{Message: "Satellite not found", Code: http.StatusNotFound})
		return
	}

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		HandleAppError(w, &AppError{Message: "Robot account not found", Code: http.StatusNotFound})
		return
	}

	newSecret, err := utils.GenerateRandomSecret()
	if err != nil {
		log.Printf("Error generating new secret: %v", err)
		HandleAppError(w, &AppError{Message: "Internal Server Error", Code: http.StatusInternalServerError})
		return
	}

	robotID, err := strconv.ParseInt(robot.RobotID, 10, 64)
	if err != nil {
		log.Printf("Error parsing robot ID: %v", err)
		HandleAppError(w, &AppError{Message: "Invalid robot ID", Code: http.StatusInternalServerError})
		return
	}

	_, err = harbor.RefreshRobotAccount(r.Context(), newSecret, robotID)
	if err != nil {
		log.Printf("Error refreshing robot account in Harbor: %v", err)
		HandleAppError(w, &AppError{Message: "Failed to refresh credentials in Harbor", Code: http.StatusInternalServerError})
		return
	}

	resp := RefreshCredentialsResponse{
		RobotName: robot.RobotName,
		Secret:    newSecret,
	}

	WriteJSONResponse(w, http.StatusOK, resp)
}
