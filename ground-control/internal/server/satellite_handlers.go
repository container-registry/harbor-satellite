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

// Constants for error messages
const (
	invalidNameMessage = "Invalid %s name. Name must contain only lowercase letters, numbers, hyphens, and periods."
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
		HandleAppError(w, ValidationError(
			fmt.Sprintf(invalidNameMessage, "satellite"),
			nil,
		))
		return
	}

	if !utils.IsValidName(req.ConfigName) {
		HandleAppError(w, ValidationError(
			"Invalid or empty config_name",
			nil,
		))
		return
	}

	// If the robot account is already present, we need to check if the robot account
	// permissions need to be updated.
	// i.e, check if the satellite is already connected to the groups in the request body.
	// if not, then update the robot account.
	roboPresent, err := harbor.IsRobotPresent(r.Context(), req.Name)
	if err != nil {
		log.Println(err)
		HandleAppError(w, ExternalAPIError(
			"Harbor",
			"querying for robot account",
			err,
		))
		return
	}

	if roboPresent {
		HandleAppError(w, ValidationError(
			"Robot Account name already present",
			nil,
		).WithSuggestion("Try with a different name"))
		return
	}

	// Start a new transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, DatabaseError("begin transaction", err))
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)
	committed := false

	// Ensure proper transaction handling with defer
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, DatabaseError("rollback transaction", err))
			}
		}
	}()

	// Create satellite
	satellite, err := q.CreateSatellite(r.Context(), req.Name)
	if err != nil {
		log.Println("Error creating satellite:", err)
		HandleAppError(w, DatabaseError("create satellite", err))
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
		HandleAppError(w, ExternalAPIError("Harbor", "creating robot account", err))
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

	config, err := q.GetConfigByName(r.Context(), req.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, NotFoundError("Config", req.ConfigName, err))
		return
	}

	setSatelliteConfigParams := database.SetSatelliteConfigParams{
		SatelliteID: satellite.ID,
		ConfigID:    config.ID,
	}

	if err := q.SetSatelliteConfig(r.Context(), setSatelliteConfigParams); err != nil {
		log.Println(err)
		HandleAppError(w, DatabaseError("set satellite config", err))
		return
	}

	// Create the satellite's state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), req.Name, groupStates, req.ConfigName)
	if err != nil {
		log.Println(err)
		HandleAppError(w, ExternalAPIError("Harbor", "create satellite state artifact", err))
		return
	}

	// Add token to DB
	token, err := GenerateRandomToken(32)
	if err != nil {
		log.Println(err)
		HandleAppError(w, NewAppError(
			"Failed to generate token",
			http.StatusInternalServerError,
			CategoryInternal,
			err,
		))
		return
	}

	tk, err := q.AddToken(r.Context(), database.AddTokenParams{
		SatelliteID: satellite.ID,
		Token:       token,
	})
	if err != nil {
		log.Println("error in token")
		log.Println(err)
		HandleAppError(w, DatabaseError("add token", err))
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, DatabaseError("commit transaction", err))
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
		log.Println("Invalid Satellite Token")
		log.Println(err)
		HandleAppError(w, ValidationError("Invalid token", err))
		return
	}

	robot, err := q.GetRobotAccBySatelliteID(r.Context(), satelliteID)
	if err != nil {
		log.Println("Robot Account Not Found")
		log.Println(err)
		HandleAppError(w, NotFoundError("Robot account", fmt.Sprintf("for satellite ID %d", satelliteID), err))
		return
	}

	// groups attached to satellite
	groups, err := q.SatelliteGroupList(r.Context(), satelliteID)
	if err != nil {
		log.Printf("failed to list groups for satellite: %v, %v", satelliteID, err)
		log.Println(err)
		HandleAppError(w, DatabaseError("list satellite groups", err))
		return
	}

	states, err := getGroupStates(r.Context(), groups, q)
	if err != nil {
		log.Println("Error retrieving group states:", err)
		HandleAppError(w, err)
		return
	}

	satellite, err := q.GetSatellite(r.Context(), satelliteID)
	if err != nil {
		HandleAppError(w, NotFoundError("Satellite", fmt.Sprintf("ID %d", satelliteID), err))
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
		HandleAppError(w, ExternalAPIError("Harbor", "update satellite state artifact", err))
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
		HandleAppError(w, DatabaseError("delete token", err))
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listSatelliteHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListSatellites(r.Context())
	if err != nil {
		log.Printf("Error: Failed to List Satellites: %v", err)
		HandleAppError(w, DatabaseError("list satellites", err))
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
		HandleAppError(w, NotFoundError("Satellite", satellite, err))
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
				HandleAppError(w, DatabaseError("rollback transaction", err))
			}
		}
	}()

	sat, err := q.GetSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to get satellite by name: %v", err)
		HandleAppError(w, NotFoundError("Satellite", satellite, err))
		return
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("error: robotAcc for satellite does not exist: %v", err)
		HandleAppError(w, NotFoundError("Robot account", fmt.Sprintf("for satellite %s", satellite), err))
		return
	}

	robotID, err := strconv.ParseInt(robotAcc.RobotID, 10, 64)
	if err != nil {
		log.Printf("error: Invalid robot ID: %v", err)
		HandleAppError(w, ValidationError(fmt.Sprintf("Invalid robot ID: %s", robotAcc.RobotID), err))
		return
	}

	err = q.DeleteSatelliteByName(r.Context(), satellite)
	if err != nil {
		log.Printf("error: failed to delete satellite: %v", err)
		HandleAppError(w, DatabaseError("delete satellite", err))
		return
	}

	_, err = harbor.DeleteRobotAccount(r.Context(), robotID)
	if err != nil {
		log.Printf("error: failed to delete robot account: %v", err)
		HandleAppError(w, ExternalAPIError("Harbor", "delete robot account", err))
		return
	}

	err = utils.DeleteArtifact(utils.ConstructHarborDeleteURL(fmt.Sprintf("satellite-state/%s/state", sat.Name)))
	if err != nil {
		log.Println(err)
		HandleAppError(w, ExternalAPIError("Harbor", "delete satellite state artifact", err))
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, DatabaseError("commit transaction", err))
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
		HandleAppError(w, ValidationError(
			fmt.Sprintf(invalidNameMessage, "satellite"),
			nil,
		))
		return
	}

	if !utils.IsValidName(req.Group) {
		HandleAppError(w, ValidationError(
			fmt.Sprintf(invalidNameMessage, "group"),
			nil,
		))
		return
	}

	// Get satellite by name
	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		HandleAppError(w, NotFoundError("Satellite", req.Satellite, err))
		return
	}

	// Get group by name
	grp, err := s.dbQueries.GetGroupByName(r.Context(), req.Group)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		HandleAppError(w, NotFoundError("Group", req.Group, err))
		return
	}

	// Check if satellite is already in the group
	alreadyInGroup, err := s.dbQueries.CheckSatelliteInGroup(r.Context(), database.CheckSatelliteInGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	})
	if err != nil {
		log.Printf("Error: Failed to check satellite in group %v", err)
		HandleAppError(w, DatabaseError("check satellite in group", err))
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
		HandleAppError(w, DatabaseError("begin transaction", err))
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, DatabaseError("rollback transaction", err))
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
		HandleAppError(w, DatabaseError("add satellite to group", err))
		return
	}

	// Get updated group list after adding the new group
	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get updated satellite group list: %v", err)
		HandleAppError(w, DatabaseError("list satellite groups", err))
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		grp, err := s.dbQueries.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed to get group by ID %d: %v", group.GroupID, err)
			HandleAppError(w, DatabaseError("get group details", err))
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
		HandleAppError(w, NotFoundError("Robot account", fmt.Sprintf("for satellite %s", req.Satellite), err))
		return
	}

	// Update robot account permissions
	_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
	if err != nil {
		log.Printf("Error: Failed to update robot account permissions: %v", err)
		HandleAppError(w, ExternalAPIError("Harbor", "update robot account permissions", err))
		return
	}

	// Update the state artifact to also track the new group state artifact
	err = utils.CreateOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, configObject.ConfigName)
	if err != nil {
		log.Printf("Error: Failed to update satellite state artifact: %v", err)
		HandleAppError(w, ExternalAPIError("Harbor", "update satellite state artifact", err))
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, DatabaseError("commit transaction", err))
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
		HandleAppError(w, DatabaseError("begin transaction", err))
		return
	}

	// Create a new Queries object bound to the transaction
	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction: %v", err)
				HandleAppError(w, DatabaseError("rollback transaction", err))
			}
		}
	}()

	sat, err := q.GetSatelliteByName(r.Context(), req.Satellite)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		HandleAppError(w, NotFoundError("Satellite", req.Satellite, err))
		return
	}

	grp, err := q.GetGroupByName(r.Context(), req.Group)
	if err != nil {
		log.Printf("Error: Group Not Found: %v", err)
		HandleAppError(w, NotFoundError("Group", req.Group, err))
		return
	}

	params := database.RemoveSatelliteFromGroupParams{
		SatelliteID: int32(sat.ID),
		GroupID:     int32(grp.ID),
	}

	err = q.RemoveSatelliteFromGroup(r.Context(), params)
	if err != nil {
		log.Printf("error: failed to remove satellite from group: %v", err)
		HandleAppError(w, DatabaseError("remove satellite from group", err))
		return
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to Add permission to robot account: %v", err)
		HandleAppError(w, NotFoundError("Robot account", fmt.Sprintf("for satellite %s", req.Satellite), err))
		return
	}

	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed: %v", err)
		HandleAppError(w, DatabaseError("refresh satellite group list", err))
		return
	}

	var projects []string
	var groupStates []string

	for _, group := range groupList {
		grp, err := q.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("Error: Failed: %v", err)
			HandleAppError(w, DatabaseError("get group details", err))
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
		HandleAppError(w, ExternalAPIError("Harbor", "update robot account permissions", err))
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
		HandleAppError(w, ExternalAPIError("Harbor", "update satellite state artifact", err))
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, DatabaseError("commit transaction", err))
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}
