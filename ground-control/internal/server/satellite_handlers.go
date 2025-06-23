package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/gorilla/mux"
)

type SatelliteGroupParams struct {
	Satellite string `json:"satellite"`
	Group     string `json:"group"`
}

type RegisterSatelliteParams struct {
	Name       string    `json:"name"`
	Groups     *[]string `json:"groups,omitempty"`
	ConfigName string    `json:"config_name"`
}

func (s *Server) registerSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterSatelliteParams
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(req.Name) {
		err := &AppError{
			Message: fmt.Sprintf(invalidNameMessage, "satellite"),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(req.ConfigName) {
		HandleAppError(w, &AppError{
			Message: "invalid or empty config_name",
			Code:    http.StatusBadRequest,
		})
		return
	}

	// If the robot account is already present, we need to check if the robot account
	// permissions need to be updated.
	// i.e, check if the satellite is already connected to the groups in the request body.
	// if not, then update the robot account.
	roboPresent, err := harbor.IsRobotPresent(r.Context(), req.Name)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error querying for robot account: %v", err.Error()),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	if roboPresent {
		err := &AppError{
			Message: "Error: Robot Account name already present. Try with different name",
			Code:    http.StatusBadRequest,
		}
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
	committed := false
	var robotID int64

	// Ensure proper transaction handling with defer
	defer func() {
		if !committed {
			// Cleanup robot account if transaction failed
			if robotID != 0 {
				if _, delErr := harbor.DeleteRobotAccount(r.Context(), robotID); delErr != nil {
					log.Printf("Warning: Failed to cleanup robot account: %v", delErr)
				}
			}
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

	// Create satellite
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

	// Create Robot Account for Satellite
	projects := []string{"satellite"}
	rbt, err := utils.CreateRobotAccForSatellite(r.Context(), projects, satellite.Name)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error: creating robot account %v", err),
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}
	robotID = rbt.ID

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

	config, err := q.GetConfigByName(r.Context(), req.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	setSatelliteConfigParams := database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    config.ID,
	}

	if err := q.SetSatelliteConfig(r.Context(), setSatelliteConfigParams); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Create the satellite's state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), req.Name, groupStates, req.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	// Add token to DB
	token, err := GenerateRandomToken(32)
	if err != nil {
		log.Println(err)
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
		masked := fmt.Sprintf("%s…%s",
			token[:4],
			token[len(token)-4:],
		)
        log.Printf("Invalid Satellite Token %s: %v", masked, err)
		err := &AppError{
			Message: "Error: Invalid Token",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		return
	}

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satelliteID)
	if err != nil {
		log.Println("Robot Account Not Found")
		log.Println(err)
		err := &AppError{
			Message: "Error: Robot Account Not Found for Satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// groups attached to satellite
	groups, err := q.SatelliteGroupList(r.Context(), satelliteID)
	if err != nil {
		log.Printf("failed to list groups for satellite: %v, %v", satelliteID, err)
		log.Println(err)
		err := &AppError{
			Message: "Error: Satellite Groups List Failed",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
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
		HandleAppError(w, &AppError{Message: "satellite not found", Code: http.StatusNotFound})
		return
	}

	configObject, err := fetchSatelliteConfig(r.Context(), s.dbQueries, satelliteID)
	if err != nil {
		log.Printf("Error: Failed to fetch Satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	// For sanity, create (update) the state artifact during the registration process as well.
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), satellite.Name, states, configObject.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	satelliteState := utils.AssembleSatelliteState(satellite.Name)

	result := config.StateConfig{
		StateURL: satelliteState,
		RegistryCredentials: config.RegistryCredentials{
			Username: robot.RobotName,
			Password: robot.RobotSecret,
			URL:      config.URL(os.Getenv("HARBOR_URL")),
		},
	}

	err = q.DeleteToken(r.Context(), token)
	if err != nil {
		log.Println("error deleting token")
		log.Println(err)
		err := &AppError{
			Message: "Error: Error deleting token",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
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

	tx, err := s.db.BeginTx(r.Context(), nil)
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

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

	err = utils.DeleteArtifact(utils.ConstructHarborDeleteURL(sat.Name, "satellite"))
	if err != nil {
		log.Println(err)
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

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) addSatelliteToGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupParams

	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	//Validate satellite and group
	if !utils.IsValidName(req.Satellite) {
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf(invalidNameMessage, "satellite"),
			Code:    http.StatusBadRequest,
		})
		return
	}

	if !utils.IsValidName(req.Group) {
		HandleAppError(w, &AppError{
			Message: fmt.Sprintf(invalidNameMessage, "group"),
			Code:    http.StatusBadRequest,
		})
		return
	}

	// Get satellite by name
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

	// Get group by name
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

	// Check if satellite is already in the group
	alreadyInGroup, err := s.dbQueries.CheckSatelliteInGroup(r.Context(), database.CheckSatelliteInGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	})
	if err != nil {
		log.Printf("Error: Failed to check satellite in group %v", err)
		err := &AppError{
			Message: "Error: Failed to check satellite in group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	if alreadyInGroup {
		log.Printf("Satellite %s is already in group %s, no changes needed", req.Satellite, req.Group)
		WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite is already in the group"})
		return
	}

	// Start a transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		err := &AppError{
			Message: "Error: Failed to start database transaction",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

	// Add satellite to group
	params := database.AddSatelliteToGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	err = q.AddSatelliteToGroup(r.Context(), params)
	if err != nil {
		log.Printf("Error: Failed to add satellite to group: %v", err)
		err := &AppError{
			Message: "Error: Failed to add satellite to group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Get updated group list after adding the new group
	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get updated satellite group list: %v", err)
		err := &AppError{
			Message: "Error: Failed to get updated satellite group list",
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
			log.Printf("Error: Failed to get group by ID %d: %v", group.GroupID, err)
			err := &AppError{
				Message: "Error: Failed to get group details",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
		projects = append(projects, grp.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	configObject, err := fetchSatelliteConfig(r.Context(), s.dbQueries, sat.ID)
	if err != nil {
		log.Printf("Error: Failed to fetch Satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	// Get robot account permissions
	robotAcc, err := s.dbQueries.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get robot account for satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to get robot account for satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Update robot account permissions
	_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
	if err != nil {
		log.Printf("Error: Failed to update robot account permissions: %v", err)
		err := &AppError{
			Message: "Error: Failed to update robot account permissions",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, configObject.ConfigName)
	if err != nil {
		log.Printf("Error: Failed to update satellite state artifact: %v", err)
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

	WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite successfully added to group"})
}

// If the satellite is removed from the group, the state artifact must be updated accordingly as well.
func (s *Server) removeSatelliteFromGroup(w http.ResponseWriter, r *http.Request) {
	var req SatelliteGroupParams
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		err := &AppError{
			Message: "Error: Failed to start database transaction",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, &AppError{
					Message: "Error: Failed to rollback transaction",
					Code:    http.StatusInternalServerError,
				})
			}
		}
	}()

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

	configObject, err := fetchSatelliteConfig(r.Context(), q, sat.ID)
	if err != nil {
		log.Printf("Error: Failed to fetch Satellite config: %v", err)
		HandleAppError(w, err)
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, configObject.ConfigName)
	if err != nil {
		log.Println(err)
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

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
