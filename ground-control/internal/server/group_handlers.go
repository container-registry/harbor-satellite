package server

import (
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
		log.Println("Error decoding request body:", err)
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println("Could not begin transaction:", err)
		HandleAppError(w, DatabaseError("begin transaction", err))
		return
	}

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				HandleAppError(w, DatabaseError("rollback transaction", err))
				return
			}
		}
	}()

	q := s.dbQueries.WithTx(tx)

	projects := utils.GetProjectNames(&req.Artifacts)
	params := database.CreateGroupParams{
		GroupName:   req.Group,
		RegistryUrl: os.Getenv("HARBOR_URL"),
		Projects:    projects,
	}
	result, err := q.CreateGroup(r.Context(), params)
	if err != nil {
		log.Println("Error creating group:", err)
		HandleAppError(w, DatabaseError("create group", err))
		return
	}

	satellites, err := q.GroupSatelliteList(r.Context(), result.ID)
	if err != nil {
		log.Println("Error listing group satellites:", err)
		HandleAppError(w, DatabaseError("list group satellites", err))
		return
	}

	for _, satellite := range satellites {
		robotAcc, err := q.GetRobotAccBySatelliteID(r.Context(), satellite.SatelliteID)
		if err != nil {
			log.Println("Error getting robot account by satellite ID:", err)
			HandleAppError(w, DatabaseError("get robot account", err))
			return
		}

		_, err = utils.UpdateRobotProjects(r.Context(), projects, robotAcc.RobotID)
		if err != nil {
			log.Println("Error updating robot projects:", err)
			HandleAppError(w, ExternalAPIError("Harbor", "update robot projects", err))
			return
		}
	}

	satExist, err := harbor.GetProject(r.Context(), "satellite")
	if err != nil {
		log.Println("Error checking satellite project existence:", err)
		HandleAppError(w, ExternalAPIError("Harbor", "checking satellite project", err))
		return
	}

	if !satExist {
		_, err := harbor.CreateSatelliteProject(r.Context())
		if err != nil {
			log.Println("Error creating satellite project:", err)
			HandleAppError(w, ExternalAPIError("Harbor", "creating satellite project", err))
			return
		}
	}

	err = utils.CreateStateArtifact(r.Context(), &req)
	if err != nil {
		log.Println("Error creating state artifact:", err)
		HandleAppError(w, ExternalAPIError("Harbor", "create state artifact", err))
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Commit failed: %v", err)
		HandleAppError(w, DatabaseError("commit transaction", err))
		return
	}
	committed = true

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) getGroupHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	group := vars["group"]

	result, err := s.dbQueries.GetGroupByName(r.Context(), group)
	if err != nil {
		log.Printf("Could not get group: %v", err)
		HandleAppError(w, NotFoundError("Group", group, err))
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) listGroupHandler(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListGroups(r.Context())
	if err != nil {
		log.Printf("Could not list groups: %v", err)
		HandleAppError(w, DatabaseError("list groups", err))
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// groupSatelliteHandler lists all satellites attached to a specific group
func (s *Server) groupSatelliteListHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupName := vars["group"]

	q := s.dbQueries

	// Get the group by name
	exists, err := q.CheckGroupExists(r.Context(), groupName)
	if err != nil {
		log.Printf("error: failed to get group : %v", err)
		HandleAppError(w, DatabaseError("check group exists", err))
		return
	}

	if !exists {
		HandleAppError(w, NotFoundError("Group", groupName, nil))
		return
	}

	// Get all satellites attached to this group
	satellites, err := q.GetSatellitesByGroupName(r.Context(), groupName)
	if err != nil {
		log.Printf("error: failed to get satellites for group: %v", err)
		HandleAppError(w, DatabaseError("get satellites for group", err))
		return
	}

	// Return empty array if no satellites in group
	if len(satellites) == 0 {
		WriteJSONResponse(w, http.StatusOK, []database.Satellite{})
		return
	}

	WriteJSONResponse(w, http.StatusOK, satellites)
}
