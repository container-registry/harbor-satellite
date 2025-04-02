package server

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/gorilla/mux"
)

type SatelliteGroupParams struct {
	Satellite string `json:"satellite"`
	Group     string `json:"group"`
}

type RegisterSatelliteParams struct {
	Name   string    `json:"name"`
	Groups *[]string `json:"groups,omitempty"`
}

// Start a new transaction. Returns a tx object and a queries object tied to the transaction.
func (s *Server) startTransaction(r *http.Request) (*sql.Tx, *database.Queries, error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, nil, err
	}
	return tx, s.dbQueries.WithTx(tx), nil
}

func (s *Server) registerSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if err := validateRequestBody(w, req); err != nil {
		HandleAppError(w, err)
		return
	}

	if err := checkRobotAccountExistence(r.Context(), req.Name); err != nil {
		HandleAppError(w, err)
		return
	}

	tx, q, err := s.startTransaction(r)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	defer func() {
		if p := recover(); p != nil {
			// If there's a panic, rollback the transaction
			tx.Rollback()
		} else if err != nil {
			tx.Rollback() // Rollback transaction on error
		}
	}()

	// Create satellite
	satellite, err := q.CreateSatellite(r.Context(), req.Name)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error: %v", err.Error()),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	groupStates, err := addSatelliteToGroups(r.Context(), q, req.Groups, satellite.ID)
	if err != nil {
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	if err := ensureSatelliteProjectExists(r.Context()); err != nil {
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	// Create Robot Account for Satellite
	projects := []string{"satellite"}
	rbt, err := utils.CreateRobotAccForSatellite(r.Context(), projects, satellite.Name)
	if err != nil {
		log.Println(err)
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf("Error: creating robot account %v", err),
			Code:    http.StatusBadRequest,
		})
		tx.Rollback()
		return
	}

	if err := storeRobotAccountInDB(r.Context(), q, rbt, satellite.ID); err != nil {
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	if err := assignPermissionsToRobot(r.Context(), q, req.Groups, rbt.ID); err != nil {
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	// Create the satellite's state artifact
	if err = utils.CreateOrUpdateSatStateArtifact(req.Name, groupStates); err != nil {
		log.Println(err)
		tx.Rollback()
		HandleAppError(w, err)
		return
	}

	// Add token to DB
	token, err := GenerateRandomToken(32)
	if err != nil {
		log.Println(err)
		tx.Rollback()
		HandleAppError(w, err)
		return
	}
	tk, err := q.AddToken(r.Context(), database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
	})
	if err != nil {
		log.Println("error in token")
		log.Println(err)
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, tk)
}

func (s *Server) ztrHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]

	tx, q, err := s.startTransaction(r)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	defer func() {
		if p := recover(); p != nil {
			// If there's a panic, rollback the transaction
			tx.Rollback()
			panic(p) // Re-throw the panic after rolling back
		} else if err != nil {
			tx.Rollback() // Rollback transaction on error
		}
	}()

	satelliteID, err := q.GetSatelliteIDByToken(r.Context(), token)
	if err != nil {
		log.Println("Invalid Satellite Token")
		log.Println(err)
		HandleAppError(w, &AppError{
			Message: "Error: Invalid Token",
			Code:    http.StatusBadRequest,
		})
		tx.Rollback()
		return
	}

	if err := q.DeleteToken(r.Context(), token); err != nil {
		log.Println("error deleting token")
		log.Println(err)
		HandleAppError(w, &AppError{
			Message: "Error: Error deleting token",
			Code:    http.StatusInternalServerError,
		})
		tx.Rollback()
		return
	}

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satelliteID)
	if err != nil {
		log.Println("Robot Account Not Found")
		log.Println(err)
		HandleAppError(w, &AppError{
			Message: "Error: Robot Account Not Found for Satellite",
			Code:    http.StatusInternalServerError,
		})
		tx.Rollback()
		return
	}

	// groups attached to satellite
	groups, err := q.SatelliteGroupList(r.Context(), satelliteID)
	if err != nil {
		log.Printf("failed to list groups for satellite: %v, %v", satelliteID, err)
		log.Println(err)
		HandleAppError(w, &AppError{
			Message: "Error: Satellite Groups List Failed",
			Code:    http.StatusInternalServerError,
		})
		tx.Rollback()
		return
	}

	states, err := getGroupStates(r.Context(), groups, q)
	if err != nil {
		HandleAppError(w, &AppError{
			Message: "Error: Get Group By ID Failed",
			Code:    http.StatusInternalServerError,
		})
		tx.Rollback()
		return
	}

	satellite, err := q.GetSatellite(r.Context(), satelliteID)
	if err != nil {
		log.Println(err)
		tx.Rollback()
		HandleAppError(w, err)
		return
	}

	// For sanity, create (update) the state artifact during the registration process as well.
	err = utils.CreateOrUpdateSatStateArtifact(satellite.Name, states)
	if err != nil {
		log.Println(err)
		tx.Rollback()
		HandleAppError(w, err)
		return
	}

	satelliteState := utils.AssembleSatelliteState(satellite.Name)

	// we need to update the state here to reflect the satellite's state artifact
	result := models.ZtrResult{
		State: satelliteState,
		Auth: models.Account{
			Name:     robot.RobotName,
			Secret:   robot.RobotSecret,
			Registry: os.Getenv("HARBOR_URL"),
		},
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
		log.Printf("Error: Failed to List Satellites: %v", err)
		err := &AppError{
			Message: "Error: Failed to List Satellites",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) GetSatelliteByName(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satellite := vars["satellite"]

	result, err := s.dbQueries.GetSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to get satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to Get Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// The state artifact corresponding to the satellite must be deleted.
func (s *Server) DeleteSatelliteByName(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	satellite := vars["satellite"]

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to get satellite by name: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	robotAcc, err := s.dbQueries.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("error: robotAcc for satellite does not exist: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	robotID, err := strconv.ParseInt(robotAcc.RobotID, 10, 64)
	if err != nil {
		log.Printf("error: Invalid robot ID: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	_, err = harbor.DeleteRobotAccount(r.Context(), robotID)
	if err != nil {
		log.Printf("error: failed to delete robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	err = s.dbQueries.DeleteSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to delete satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	err = utils.DeleteSatelliteStateArtifact(satellite)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

// Once a satellite is added to the group, the satellite's stateartifact must be updated accordingly.
func (s *Server) addSatelliteToGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	grp, err := s.dbQueries.GetGroupByName(r.Context(), req.Group)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		err := &AppError{
			Message: "Error: Group Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	params := database.AddSatelliteToGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	err = s.dbQueries.AddSatelliteToGroup(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to Add Satellite to Group: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add Satellite to Group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	robotAcc, err := s.dbQueries.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	groupList, err := s.dbQueries.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add satellite to group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		grp, err := s.dbQueries.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed: %v", err)
			err := &AppError{
				Message: "Error: Failed to Add satellite to group",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		projects = append(projects, grp.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(sat.Name, groupStates)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

// If the satellite is removed from the group, the state artifact must be updated accordingly as well.
func (s *Server) removeSatelliteFromGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	grp, err := s.dbQueries.GetGroupByName(r.Context(), req.Group)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		err := &AppError{
			Message: "Error: Group Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	params := database.RemoveSatelliteFromGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	err = s.dbQueries.RemoveSatelliteFromGroup(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to remove satellite from group: %v", err)
		err := &AppError{
			Message: "Error: Failed to Remove Satellite from Group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	robotAcc, err := s.dbQueries.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	groupList, err := s.dbQueries.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed: %v", err)
		err := &AppError{
			Message: "Error: Failed to refresh satellite group list",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		grp, err := s.dbQueries.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed: %v", err)
			err := &AppError{
				Message: "Error: Failed to to refresh satellite group list",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		projects = append(projects, grp.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	// 1. We need the list of state artifacts for the groups that satellite belongs to
	// 2. Update the satellite state artifact accordingly

	_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to update robot account permissions",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(sat.Name, groupStates)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
