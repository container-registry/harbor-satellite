package server

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/gorilla/mux"
)

func (s *Server) groupsSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req models.StateArtifact
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Start a new transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)
	// Ensure proper transaction handling with defer
	defer func() {
		if p := recover(); p != nil {
			// If there's a panic, rollback the transaction
			tx.Rollback()
		} else if err != nil {
			tx.Rollback() // Rollback transaction on error
		}
	}()
	projects := utils.GetProjectNames(&req.Artifacts)
	params := database.CreateGroupParams{
		GroupName:   req.Group,
		RegistryUrl: os.Getenv("HARBOR_URL"),
		Projects:    projects,
	}
	result, err := q.CreateGroup(r.Context(), params)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	satellites, err := q.GroupSatelliteList(r.Context(), result.ID)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	for _, satellite := range satellites {
		robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), satellite.SatelliteID)
		if err != nil {
			log.Println(err)
			HandleAppError(w, err)
			return
		}
		// update robot account projects permission
		_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
		if err != nil {
			log.Println(err)
			HandleAppError(w, err)
			return
		}
	}

	// check if project satellite exists and if does not exist create project satellite
	satExist, err := harbor.GetProject(r.Context(), "satellite")
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error: Checking satellite project: %v", err),
			Code:    http.StatusBadGateway,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
	}
	if !satExist {
		_, err := harbor.CreateSatelliteProject(r.Context())
		if err != nil {
			log.Println(err)
			err := &AppError{
				Message: fmt.Sprintf("Error: creating satellite project: %v", err),
				Code:    http.StatusBadGateway,
			}
			HandleAppError(w, err)
			tx.Rollback()
			return
		}
	}

	// Create State Artifact for the group
	err = utils.CreateStateArtifact(r.Context(), &req)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	group := vars["group"]

	result, err := s.dbQueries.GetGroupByName(r.Context(), group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listGroupHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListGroups(r.Context())
	if err != nil {
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}
