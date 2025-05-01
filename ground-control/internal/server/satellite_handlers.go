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
func (s *Server) initTransaction(r *http.Request) (*sql.Tx, *database.Queries, error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, nil, err
	}
	return tx, s.dbQueries.WithTx(tx), nil
}

func (s *Server) registerSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterSatelliteParams
	var err error

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request body:", err)
		HandleAppError(w, err)
		return
	}

	if err := validateRequestBody(w, req); err != nil {
		log.Println("Error validating request body:", err)
		HandleAppError(w, err)
		return
	}

	if err := checkRobotAccountExistence(r.Context(), req.Name); err != nil {
		HandleAppError(w, err)
		return
	}

	tx, q, err := s.initTransaction(r)
	if err != nil {
		log.Println("Error starting transaction:", err)
		HandleAppError(w, err)
		return
	}

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				err := &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				}
				HandleAppError(w, err)
				return
			}
		}
	}()

	satellite, err := q.CreateSatellite(r.Context(), req.Name)
	if err != nil {
		log.Println("Error creating satellite:", err)
		err := &AppError{
			Message: fmt.Sprintf("Error: %v", err.Error()),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	groupStates, err := addSatelliteToGroups(r.Context(), q, req.Groups, satellite.ID)
	if err != nil {
		log.Println("Error adding satellite to groups:", err)
		HandleAppError(w, err)
		return
	}

	if err := ensureSatelliteProjectExists(r.Context()); err != nil {
		log.Println("Error ensuring satellite project exists:", err)
		HandleAppError(w, err)
		return
	}

	projects := []string{"satellite"}
	rbt, err := utils.CreateRobotAccForSatellite(r.Context(), projects, satellite.Name)
	if err != nil {
		log.Println("Error creating robot account for satellite:", err)
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf("Error: creating robot account %v", err),
			Code:    http.StatusBadRequest,
		})
		return
	}

	if err := storeRobotAccountInDB(r.Context(), q, rbt, satellite.ID); err != nil {
		log.Println("Error storing robot account in DB:", err)
		HandleAppError(w, err)
		return
	}

	if err := assignPermissionsToRobot(r.Context(), q, req.Groups, rbt.ID); err != nil {
		log.Println("Error assigning permissions to robot:", err)
		HandleAppError(w, err)
		return
	}

	if err = utils.CreateOrUpdateSatStateArtifact(r.Context(), req.Name, groupStates); err != nil {
		log.Println("Error creating/updating satellite state artifact:", err)
		HandleAppError(w, err)
		return
	}

	token, err := GenerateRandomToken(32)
	if err != nil {
		log.Println("Error generating random token:", err)
		HandleAppError(w, err)
		return
	}

	tk, err := q.AddToken(r.Context(), database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
	})
	if err != nil {
		log.Println("Error adding token to database:", err)
		HandleAppError(w, err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Could not commit transaction",
			Code:    http.StatusInternalServerError,
		})
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, tk)
}

func (s *Server) ztrHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]

	q := s.dbQueries

	satelliteID, err := q.GetSatelliteIDByToken(r.Context(), token)
	if err != nil {
		log.Println("Error retrieving satellite ID by token:", err)
		HandleAppError(w, &AppError{
			Message: "Error: Invalid Token",
			Code:    http.StatusBadRequest,
		})
		return
	}

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satelliteID)
	if err != nil {
		log.Println("Error retrieving robot account for satellite:", err)
		HandleAppError(w, &AppError{
			Message: "Error: Robot Account Not Found for Satellite",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	groups, err := q.SatelliteGroupList(r.Context(), satelliteID)
	if err != nil {
		log.Printf("Error listing groups for satellite %v: %v", satelliteID, err)
		HandleAppError(w, &AppError{
			Message: "Error: Satellite Groups List Failed",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	states, err := getGroupStates(r.Context(), groups, q)
	if err != nil {
		log.Println("Error retrieving group states:", err)
		HandleAppError(w, &AppError{
			Message: "Error: Get Group By ID Failed",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	satellite, err := q.GetSatellite(r.Context(), satelliteID)
	if err != nil {
		log.Println("Error retrieving satellite:", err)
		HandleAppError(w, err)
		return
	}

	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), satellite.Name, states)
	if err != nil {
		log.Println("Error creating/updating satellite state artifact:", err)
		HandleAppError(w, err)
		return
	}

	satelliteState := utils.AssembleSatelliteState(satellite.Name)

	result := models.ZtrResult{
		State: satelliteState,
		Auth: models.Account{
			Name:     robot.RobotName,
			Secret:   robot.RobotSecret,
			Registry: os.Getenv("HARBOR_URL"),
		},
	}

	if err := q.DeleteToken(r.Context(), token); err != nil {
		log.Println("Error deleting token:", err)
		HandleAppError(w, &AppError{
			Message: "Error: Error deleting token",
			Code:    http.StatusInternalServerError,
		})
		return
	}

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

	q := s.dbQueries

	sat, err := q.GetSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to get satellite by name: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
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

	err = utils.DeleteSatelliteStateArtifact(satellite)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	err = q.DeleteSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to delete satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to Delete Satellite",
			Code:    http.StatusInternalServerError,
		}
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

	q := s.dbQueries

	sat, err := q.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		})
		return
	}

	grp, err := q.GetGroupByName(r.Context(), req.Group)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Group Not Found",
			Code:    http.StatusBadRequest,
		})
		return
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get robot account: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to list satellite groups: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to Add satellite to group",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		g, err := q.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed to fetch group details: %v", err)
			HandleAppError(w, &AppError{
				Message: "Error: Failed to Add satellite to group",
				Code:    http.StatusInternalServerError,
			})
			return
		}
		projects = append(projects, g.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(g.GroupName))
	}

	if _, err := utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID); err != nil {
		log.Printf("Error: Failed to update robot account projects: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		})
		return
	}

	if err := utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates); err != nil {
		log.Printf("Error: Failed to update state artifact: %v", err)
		HandleAppError(w, err)
		return
	}

	params := database.AddSatelliteToGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	if err := q.AddSatelliteToGroup(r.Context(), params); err != nil {
		log.Printf("Error: Failed to add satellite to group: %v", err)
		HandleAppError(w, &AppError{
			Message: "Error: Failed to Add Satellite to Group",
			Code:    http.StatusInternalServerError,
		})
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

	q := s.dbQueries

	sat, err := q.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		err := &AppError{
			Message: "Error: Satellite Not Found",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		err := &AppError{
			Message: "Error: Failed to Add permission to robot account",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
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
		grp, err := q.GetGroupByID(r.Context(), group.GroupID)
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
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	grp, err := q.GetGroupByName(r.Context(), req.Group)
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

	err = q.RemoveSatelliteFromGroup(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to remove satellite from group: %v", err)
		err := &AppError{
			Message: "Error: Failed to Remove Satellite from Group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
