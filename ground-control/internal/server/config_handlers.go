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
)

func (s *Server) configsSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req models.StateArtifact
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}
	q := s.dbQueries.WithTx(tx)
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
		} else if err != nil {
			tx.Rollback()
		}
	}()

	// Validate the config statically
	if err := utils.ValidateConfig(req.Artifacts); err != nil {
		log.Println("Invalid config:", err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	params := database.CreateConfigParams{
		ConfigName:  req.Group,
		RegistryUrl: os.Getenv("HARBOR_URL"),
		Projects:    utils.GetProjectNames(&req.Artifacts),
	}
	result, err := q.CreateConfig(r.Context(), params)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	satellites, err := q.ConfigSatelliteList(r.Context(), result.ID)
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
		_, err = utils.UpdateRobotProjects(r.Context(), params.Projects, robotAcc.RobotID)
		if err != nil {
			log.Println(err)
			HandleAppError(w, err)
			return
		}
	}

	satExist, err := harbor.GetProject(r.Context(), "satellite")
	if err != nil {
		err := &AppError{
			Message: fmt.Sprintf("Error: Checking satellite project: %v", err),
			Code:    http.StatusBadGateway,
		}
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}
	if !satExist {
		_, err := harbor.CreateSatelliteProject(r.Context())
		if err != nil {
			err := &AppError{
				Message: fmt.Sprintf("Error: creating satellite project: %v", err),
				Code:    http.StatusBadGateway,
			}
			log.Println(err)
			HandleAppError(w, err)
			tx.Rollback()
			return
		}
	}

	// Upload config as OCI artifact
	err = utils.CreateStateArtifact(&req)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, result)
}
