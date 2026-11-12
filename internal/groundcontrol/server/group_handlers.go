package server

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbor"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/utils"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
)

// groupSyncHarbor is the Harbor side of SyncGroup. Tests replace it so group
// sync can be checked without a Harbor.
type groupSyncHarbor interface {
	ProjectExists(ctx context.Context, name string) (bool, error)
	CreateSatelliteProject(ctx context.Context) (bool, error)
	CreateStateArtifact(ctx context.Context, req GroupSyncRequest) error
	UpdateRobotProjects(ctx context.Context, projects []string, robotID string) error
}

type harborGroupSync struct{}

func (harborGroupSync) ProjectExists(ctx context.Context, name string) (bool, error) {
	return harbor.GetProject(ctx, name)
}

func (harborGroupSync) CreateSatelliteProject(ctx context.Context) (bool, error) {
	return harbor.CreateSatelliteProject(ctx)
}

func (harborGroupSync) CreateStateArtifact(ctx context.Context, req GroupSyncRequest) error {
	return createStateArtifact(ctx, req)
}

func (harborGroupSync) UpdateRobotProjects(ctx context.Context, projects []string, robotID string) error {
	_, err := utils.UpdateRobotProjects(ctx, projects, robotID)
	return err
}

func (s *Server) SyncGroup(w http.ResponseWriter, r *http.Request) {
	var req GroupSyncRequest

	if err := DecodeRequestBody(r, &req); err != nil {
		log.Println("Error decoding request body:", err)
		HandleAppError(w, err)
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		log.Println("Could not begin transaction:", err)
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

	if err := lockGroupMembership(r.Context(), tx); err != nil {
		HandleAppError(w, err)
		return
	}

	q := s.dbQueries.WithTx(tx)

	projects := getProjectNames(req.Artifacts)
	params := database.CreateGroupParams{
		GroupName:   req.Group,
		RegistryUrl: env.GC.Harbor.URL,
		Projects:    projects,
	}
	result, err := q.CreateGroup(r.Context(), params)
	if err != nil {
		log.Println("Error creating group:", err)
		HandleAppError(w, err)
		return
	}

	satellites, err := q.GroupSatelliteList(r.Context(), result.ID)
	if err != nil {
		log.Println("Error listing group satellites:", err)
		HandleAppError(w, err)
		return
	}

	satExist, err := s.groupSync.ProjectExists(r.Context(), "satellite")
	if err != nil {
		log.Println("Error checking satellite project existence:", err)
		err := &AppError{
			Message: fmt.Sprintf("Error: Checking satellite project: %v", err),
			Code:    http.StatusBadGateway,
		}
		HandleAppError(w, err)
		return
	}

	if !satExist {
		_, err := s.groupSync.CreateSatelliteProject(r.Context())
		if err != nil {
			log.Println("Error creating satellite project:", err)
			err := &AppError{
				Message: fmt.Sprintf("Error: creating satellite project: %v", err),
				Code:    http.StatusBadGateway,
			}
			HandleAppError(w, err)
			return
		}
	}

	err = s.groupSync.CreateStateArtifact(r.Context(), req)
	if err != nil {
		log.Println("Error creating state artifact:", err)
		HandleAppError(w, err)
		return
	}

	// Harbor keeps robot edits if this transaction rolls back, so update
	// them only after the state artifact push has succeeded.
	ids := make([]int32, 0, len(satellites))
	for _, satellite := range satellites {
		ids = append(ids, satellite.SatelliteID)
	}
	// q is the sync transaction, so the query sees the group row this
	// request just wrote.
	if err := syncRobotPermissionsWith(r.Context(), q, s.groupSync, ids); err != nil {
		HandleAppError(w, robotPermissionsError(err))
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

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) GetGroup(w http.ResponseWriter, r *http.Request, group string) {
	result, err := s.dbQueries.GetGroupByName(r.Context(), group)
	if err != nil {
		log.Printf("Could not get group: %v", err)
		WriteJSONError(w, "Group not found", http.StatusNotFound)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

func (s *Server) DeleteGroup(w http.ResponseWriter, r *http.Request, groupName string) {
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

	q := s.dbQueries.WithTx(tx)

	committed := false
	defer func() {
		if p := recover(); p != nil {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				return
			}
		} else if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("Error: Failed to rollback transaction for failed process: %v", err)
				return
			}
		}
	}()

	if err := lockGroupMembership(r.Context(), tx); err != nil {
		HandleAppError(w, err)
		return
	}

	group, err := q.GetGroupByName(r.Context(), groupName)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: "Error: Group Not Found",
			Code:    http.StatusNotFound,
		}
		HandleAppError(w, err)
		return
	}

	// Remove the group from all associated satellites
	satellites, err := q.GroupSatelliteList(r.Context(), group.ID)
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: "Error: Failed to list satellites for group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	for _, satellite := range satellites {
		if err := q.RemoveSatelliteFromGroup(r.Context(), database.RemoveSatelliteFromGroupParams{
			SatelliteID: satellite.SatelliteID,
			GroupID:     group.ID,
		}); err != nil {
			log.Println(err)
			err := &AppError{
				Message: "Error: Failed to remove group from satellite",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}

		// Get remaining groups for this satellite
		groupList, err := q.SatelliteGroupList(r.Context(), satellite.SatelliteID)
		if err != nil {
			log.Println(err)
			err := &AppError{
				Message: "Error: Failed to update satellite state",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}

		var groupStates []string
		for _, g := range groupList {
			grp, err := q.GetGroupByID(r.Context(), g.GroupID)
			if err != nil {
				log.Println(err)
				err := &AppError{
					Message: "Error: Failed to update satellite state",
					Code:    http.StatusInternalServerError,
				}
				HandleAppError(w, err)
				return
			}
			groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
		}

		if err := syncRobotPermissionsWith(r.Context(), q, s.groupSync, []int32{satellite.SatelliteID}); err != nil {
			HandleAppError(w, robotPermissionsError(err))
			return
		}

		sat, err := q.GetSatellite(r.Context(), satellite.SatelliteID)
		if err != nil {
			log.Println(err)
			err := &AppError{
				Message: "Error: Failed to update satellite state",
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

		// Update state artifact
		err = createOrUpdateSatStateArtifact(r.Context(), sat.Name, groupStates, configObject.ConfigName)
		if err != nil {
			log.Println(err)
			err := &AppError{
				Message: "Error: Failed to update satellite state",
				Code:    http.StatusInternalServerError,
			}
			HandleAppError(w, err)
			return
		}
	}

	if err := q.DeleteGroup(r.Context(), group.ID); err != nil {
		log.Println(err)
		err := &AppError{
			Message: "Error: Failed to delete group",
			Code:    http.StatusInternalServerError,
		}
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

	err = utils.DeleteArtifact(utils.ConstructHarborDeleteURL(groupName, "group"))
	if err != nil {
		log.Println(err)
		err := &AppError{
			Message: "Error: Failed to delete group state",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, map[string]string{})
}

func (s *Server) ListGroups(w http.ResponseWriter, r *http.Request) {
	result, err := s.dbQueries.ListGroups(r.Context())
	if err != nil {
		log.Printf("Could not list groups: %v", err)
		HandleAppError(w, err)
		return
	}

	WriteJSONResponse(w, http.StatusOK, result)
}

// ListGroupSatellites lists all satellites attached to a specific group
func (s *Server) ListGroupSatellites(w http.ResponseWriter, r *http.Request, groupName string) {
	q := s.dbQueries

	// Get the group by name
	exists, err := q.CheckGroupExists(r.Context(), groupName)
	if err != nil {
		log.Printf("error: failed to get group : %v", err)
		err := &AppError{
			Message: "error: failed to get group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	if !exists {
		err := &AppError{
			Message: "error: group not found",
			Code:    http.StatusNotFound,
		}
		HandleAppError(w, err)
		return
	}

	// Get all satellites attached to this group
	satellites, err := q.GetSatellitesByGroupName(r.Context(), groupName)
	if err != nil {
		log.Printf("error: failed to get satellites for group: %v", err)
		err := &AppError{
			Message: "error: failed to get satellites for group",
			Code:    http.StatusInternalServerError,
		}
		HandleAppError(w, err)
		return
	}

	// Return empty array if no satellites in group
	if len(satellites) == 0 {
		WriteJSONResponse(w, http.StatusOK, []database.Satellite{})
		return
	}

	WriteJSONResponse(w, http.StatusOK, satellites)
}
