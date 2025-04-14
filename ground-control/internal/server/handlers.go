package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/models"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	"github.com/gorilla/mux"
)

const (
	invalidNameMessage = "Invalid %s name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-"
)

type RegisterSatelliteParams struct {
	Name   string    `json:"name"`
	Groups *[]string `json:"groups,omitempty"`
}
type SatelliteGroupParams struct {
	Satellite string `json:"satellite"`
	Group     string `json:"group"`
}

func DecodeRequestBody(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return &AppError{
			Message: "Invalid request body",
			Code:    http.StatusBadRequest,
		}
	}
	return nil
}

func (s *Server) Ping(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("pong"))
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	err := s.db.Ping()
	if err != nil {
		log.Printf("error pinging db: %v", err)
		msg, _ := json.Marshal(map[string]string{"status": "unhealthy"})
		http.Error(w, string(msg), http.StatusBadRequest)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) groupsSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req models.StateArtifact
	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	if !utils.IsValidName(req.Group) {
		err := &AppError{
			Message: fmt.Sprintf(invalidNameMessage, "group"),
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
	err = utils.CreateStateArtifact(&req)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, result)
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
	// Ensure proper transaction handling with defer
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
	var groupStates []string

	// Check if Groups is nil before dereferencing
	if req.Groups != nil {
		// Add satellite to groups
		for _, groupName := range *req.Groups {
			// check if groups are declared in replication
			replications, err := harbor.ListReplication(r.Context(), harbor.ListParams{
				Q: fmt.Sprintf("name=%s", groupName),
			})
			if len(replications) < 1 {
				if err != nil {
					log.Println(err)
					err := &AppError{
						Message: fmt.Sprintf("Error: Group Name: %s, does not exist in replication, Please give a Valid Group Name", groupName),
						Code:    http.StatusBadRequest,
					}
					HandleAppError(w, err)
					tx.Rollback()
					return
				}
			}
			group, err := q.GetGroupByName(r.Context(), groupName)
			if err != nil {
				log.Println(err)
				err := &AppError{
					Message: fmt.Sprintf("Error: Invalid Group Name: %v", groupName),
					Code:    http.StatusBadRequest,
				}
				HandleAppError(w, err)
				tx.Rollback()
				return
			}
			err = q.AddSatelliteToGroup(r.Context(), database.AddSatelliteToGroupParams{
				SatelliteID: satellite.ID,
				GroupID:     group.ID,
			})
			if err != nil {
				log.Println(err)
				HandleAppError(w, err)
				tx.Rollback()
				return
			}
			groupStates = append(groupStates, utils.AssembleGroupState(groupName))
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
		tx.Rollback()
		return
	}
	// Add Robot Account to database
	params := database.AddRobotAccountParams{
		RobotName:   rbt.Name,
		RobotSecret: rbt.Secret,
		RobotID:     strconv.Itoa(int(rbt.ID)),
		SatelliteID: satellite.ID,
	}
	_, err = q.AddRobotAccount(r.Context(), params)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: fmt.Sprintf("Error: adding robot account to DB %v", err.Error()),
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
	}

	// Give permission to the robot account for the projects present in the group list
	// fetch all the projects
	for i := range *req.Groups {
		projects, err := q.GetProjectsOfGroup(r.Context(), (*req.Groups)[i])
		if err != nil {
			log.Println(err)
			err := &AppError{
				Message: fmt.Sprintf("Error: fetching projects of group %v", err.Error()),
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			tx.Rollback()
			return
		}
		project := projects[0]

		// give permission to the robot account for all the projects present in this group
		_, err = utils.UpdateRobotProjects(r.Context(), project, strconv.FormatInt(rbt.ID, 10))
		if err != nil {
			log.Println(err)
			err := &AppError{
				Message: fmt.Sprintf("Error: updating robot account %v", err.Error()),
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			tx.Rollback()
			return
		}

	}

	// Create the satellite's state artifact
	err = utils.CreateOrUpdateSatStateArtifact(req.Name, groupStates)
	if err != nil {
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

	// Start a new transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println(err)
		HandleAppError(w, err)
		return
	}

	q := s.dbQueries.WithTx(tx)

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
		err := &AppError{
			Message: "Error: Invalid Token",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
		tx.Rollback()
		return
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
		tx.Rollback()
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
		tx.Rollback()
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
		tx.Rollback()
		return
	}

	var states []string
	for _, group := range groups {
		grp, err := q.GetGroupByID(r.Context(), group.GroupID)
		if err != nil {
			log.Printf("failed to get group by ID: %v, %v", group.GroupID, err)
			log.Println(err)
			err := &AppError{
				Message: "Error: Get Group By ID Failed",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			tx.Rollback()
			return
		}
		state := utils.AssembleGroupState(grp.GroupName)
		states = append(states, state)
	}

	satellite, err := q.GetSatellite(r.Context(), satelliteID)

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

	if req.Satellite == "" || req.Group == "" {
		err := &AppError{
			Message: "Error: Satellite and Group names cannot be empty",
			Code:    http.StatusBadRequest,
		}
		HandleAppError(w, err)
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

	// Ensure proper transaction handling with defer
	defer func() {
		if p := recover(); p != nil {
			// If there's a panic, rollback the transaction
			tx.Rollback()
		} else if err != nil {
			tx.Rollback() // Rollback transaction on error
		}
	}()

	// Get satellite by name
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

	// Get group by name
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

	// Check if satellite is already in the group
	groupList, err := q.SatelliteGroupList(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get satellite group list: %v", err)
		err := &AppError{
			Message: "Error: Failed to get satellite group list",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	alreadyInGroup := false
	for _, group := range groupList {
		if group.GroupID == grp.ID {
			alreadyInGroup = true
			break
		}
	}

	if alreadyInGroup {
		log.Printf("Satellite %s is already in group %s, no changes needed", req.Satellite, req.Group)
		WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite is already in the group"})
		return
	}

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

	// Get robot account for the satellite
	robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get robot account for satellite: %v", err)
		err := &AppError{
			Message: "Error: Failed to get robot account for satellite",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Get updated group list after adding the new group
	groupList, err = q.SatelliteGroupList(r.Context(), sat.ID)
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
		grp, err := q.GetGroupByID(r.Context(), group.GroupID)
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
	err = utils.CreateOrUpdateSatStateArtifact(sat.Name, groupStates)
	if err != nil {
		log.Printf("Error: Failed to update satellite state artifact: %v", err)
		HandleAppError(w, err)
		return
	}

	tx.Commit()
	WriteJSONResponse(w, http.StatusOK, map[string]string{"message": "Satellite successfully added to group"})
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

func (s *Server) listGroupHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListGroups(r.Context())
	if err != nil {
		HandleAppError(w, err)
		return
	}

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

// creates a unique random API token of the specified length in bytes.
func GenerateRandomToken(charLength int) (string, error) {
	// The number of bytes needed to generate a token with the required number of hex characters
	byteLength := charLength / 2

	// Create a byte slice of the required length
	token := make([]byte, byteLength)
	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	// Return the token as a hex-encoded string
	return hex.EncodeToString(token), nil
}

func GetAuthToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		err := &AppError{
			Message: "Authorization header missing",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		err := &AppError{
			Message: "Invalid Authorization header format",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}
	token := parts[1]

	return token, nil
}
