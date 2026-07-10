package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/env"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/harbor"
	gcmodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/models"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/groups"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/utils"
	"github.com/go-openapi/runtime/middleware"
)

func AddSatelliteToGroup(params groups.AddSatelliteToGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewAddSatelliteToGroupUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return groups.NewAddSatelliteToGroupBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}
	if !utils.IsValidName(params.Body.Satellite) {
		return groups.NewAddSatelliteToGroupBadRequest().WithPayload(appError("Invalid satellite name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-", http.StatusBadRequest))
	}
	if !utils.IsValidName(params.Body.Group) {
		return groups.NewAddSatelliteToGroupBadRequest().WithPayload(appError("Invalid group name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-", http.StatusBadRequest))
	}

	ctx := params.HTTPRequest.Context()
	sat, err := svc.queries.GetSatelliteByName(ctx, params.Body.Satellite)
	if err != nil {
		return groups.NewAddSatelliteToGroupBadRequest().WithPayload(appError("Error: Satellite Not Found", http.StatusBadRequest))
	}
	grp, err := svc.queries.GetGroupByName(ctx, params.Body.Group)
	if err != nil {
		return groups.NewAddSatelliteToGroupBadRequest().WithPayload(appError("Error: Group Not Found", http.StatusBadRequest))
	}

	alreadyInGroup, err := svc.queries.CheckSatelliteInGroup(ctx, database.CheckSatelliteInGroupParams{
		SatelliteID: sat.ID,
		GroupID:     grp.ID,
	})
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Failed to check satellite in group", http.StatusInternalServerError))
	}
	if alreadyInGroup {
		return groups.NewAddSatelliteToGroupOK().WithPayload(&swaggermodels.APIMessageResponse{Message: "Satellite is already in the group"})
	}

	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Failed to start database transaction", http.StatusInternalServerError))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	if err := q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{
		SatelliteID: sat.ID,
		GroupID:     grp.ID,
	}); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Failed to add satellite to group", http.StatusInternalServerError))
	}

	projects, groupStates, err := satelliteProjectsAndStates(ctx, q, sat.ID)
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Failed to get updated satellite group list", http.StatusInternalServerError))
	}

	configObject, errPayload := fetchSatelliteConfig(ctx, q, sat.ID)
	if errPayload != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(errPayload)
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(ctx, sat.ID)
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Failed to get robot account for satellite", http.StatusInternalServerError))
	}
	if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Failed to update robot account permissions", http.StatusInternalServerError))
	}
	if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, configObject.ConfigName); err != nil {
		log.Printf("Error: Failed to update satellite state artifact: %v", err)
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(appError("Error: Could not commit transaction", http.StatusInternalServerError))
	}
	committed = true

	return groups.NewAddSatelliteToGroupOK().WithPayload(&swaggermodels.APIMessageResponse{Message: "Satellite successfully added to group"})
}

func DeleteGroup(params groups.DeleteGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requireSystemAdmin(principal); errPayload != nil {
		if errPayload.Code == http.StatusUnauthorized {
			return groups.NewDeleteGroupUnauthorized().WithPayload(errPayload)
		}
		return groups.NewDeleteGroupForbidden().WithPayload(errPayload)
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to start database transaction", http.StatusInternalServerError))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	group, err := q.GetGroupByName(ctx, params.Group)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groups.NewDeleteGroupNotFound().WithPayload(appError("Error: Group Not Found", http.StatusNotFound))
		}
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	satellites, err := q.GroupSatelliteList(ctx, group.ID)
	if err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to list satellites for group", http.StatusInternalServerError))
	}

	for _, satellite := range satellites {
		if err := q.RemoveSatelliteFromGroup(ctx, database.RemoveSatelliteFromGroupParams{
			SatelliteID: satellite.SatelliteID,
			GroupID:     group.ID,
		}); err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to remove group from satellite", http.StatusInternalServerError))
		}

		robotAcc, err := q.GetRobotAccBySatelliteID(ctx, satellite.SatelliteID)
		if err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to update satellite permissions", http.StatusInternalServerError))
		}

		projects, groupStates, err := satelliteProjectsAndStates(ctx, q, satellite.SatelliteID)
		if err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to update satellite state", http.StatusInternalServerError))
		}

		if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to update satellite permissions", http.StatusInternalServerError))
		}

		sat, err := q.GetSatellite(ctx, satellite.SatelliteID)
		if err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to update satellite state", http.StatusInternalServerError))
		}

		configObject, errPayload := fetchSatelliteConfig(ctx, q, sat.ID)
		if errPayload != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(errPayload)
		}

		if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, configObject.ConfigName); err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to update satellite state", http.StatusInternalServerError))
		}
	}

	if err := q.DeleteGroup(ctx, group.ID); err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to delete group", http.StatusInternalServerError))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Could not commit transaction", http.StatusInternalServerError))
	}
	committed = true

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(params.Group, "group")); err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(appError("Error: Failed to delete group state", http.StatusInternalServerError))
	}

	return groups.NewDeleteGroupOK().WithPayload(map[string]string{})
}

func GetGroup(params groups.GetGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewGetGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewGetGroupUnauthorized().WithPayload(errPayload)
	}

	group, err := svc.queries.GetGroupByName(params.HTTPRequest.Context(), params.Group)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groups.NewGetGroupNotFound().WithPayload(appError("Group not found", http.StatusNotFound))
		}
		return groups.NewGetGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	return groups.NewGetGroupOK().WithPayload(apiGroup(group))
}

func ListGroupSatellites(params groups.ListGroupSatellitesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewListGroupSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewListGroupSatellitesUnauthorized().WithPayload(errPayload)
	}
	if _, err := svc.queries.GetGroupByName(params.HTTPRequest.Context(), params.Group); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groups.NewListGroupSatellitesNotFound().WithPayload(appError("Group not found", http.StatusNotFound))
		}
		return groups.NewListGroupSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	rows, err := svc.queries.GetSatellitesByGroupName(params.HTTPRequest.Context(), params.Group)
	if err != nil {
		return groups.NewListGroupSatellitesInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APIGroupSatellite, 0, len(rows))
	for _, row := range rows {
		response = append(response, apiGroupSatellite(row))
	}

	return groups.NewListGroupSatellitesOK().WithPayload(response)
}

func ListGroups(params groups.ListGroupsParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewListGroupsInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewListGroupsUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.ListGroups(params.HTTPRequest.Context())
	if err != nil {
		return groups.NewListGroupsInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APIGroup, 0, len(rows))
	for _, row := range rows {
		response = append(response, apiGroup(row))
	}

	return groups.NewListGroupsOK().WithPayload(response)
}

func RemoveSatelliteFromGroup(params groups.RemoveSatelliteFromGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewRemoveSatelliteFromGroupUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return groups.NewRemoveSatelliteFromGroupBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Error: Failed to start database transaction", http.StatusInternalServerError))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	sat, err := q.GetSatelliteByName(ctx, params.Body.Satellite)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupBadRequest().WithPayload(appError("Error: Satellite Not Found", http.StatusBadRequest))
	}
	grp, err := q.GetGroupByName(ctx, params.Body.Group)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupBadRequest().WithPayload(appError("Error: Group Not Found", http.StatusBadRequest))
	}

	if err := q.RemoveSatelliteFromGroup(ctx, database.RemoveSatelliteFromGroupParams{
		SatelliteID: sat.ID,
		GroupID:     grp.ID,
	}); err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Error: Failed to Remove Satellite from Group", http.StatusInternalServerError))
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(ctx, sat.ID)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Error: Failed to Add permission to robot account", http.StatusInternalServerError))
	}

	projects, groupStates, err := satelliteProjectsAndStates(ctx, q, sat.ID)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Error: Failed to refresh satellite group list", http.StatusInternalServerError))
	}

	if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Error: Failed to update robot account permissions", http.StatusInternalServerError))
	}

	configObject, errPayload := fetchSatelliteConfig(ctx, q, sat.ID)
	if errPayload != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(errPayload)
	}

	if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, configObject.ConfigName); err != nil {
		log.Printf("Error: Failed to update satellite state artifact: %v", err)
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(appError("Error: Could not commit transaction", http.StatusInternalServerError))
	}
	committed = true

	return groups.NewRemoveSatelliteFromGroupOK().WithPayload(map[string]string{})
}

func SyncGroup(params groups.SyncGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewSyncGroupUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return groups.NewSyncGroupBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	req := stateArtifactFromAPI(params.Body)
	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	projects := utils.GetProjectNames(&req.Artifacts)
	result, err := q.CreateGroup(ctx, database.CreateGroupParams{
		GroupName:   req.Group,
		RegistryUrl: env.GC.Harbor.URL,
		Projects:    projects,
	})
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	satellites, err := q.GroupSatelliteList(ctx, result.ID)
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	for _, satellite := range satellites {
		robotAcc, err := q.GetRobotAccBySatelliteID(ctx, satellite.SatelliteID)
		if err != nil {
			return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
		}
		if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
			return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
		}
	}

	satExist, err := harbor.GetProject(ctx, "satellite")
	if err != nil {
		return groups.NewSyncGroupBadGateway().WithPayload(appError("Error: Checking satellite project", http.StatusBadGateway))
	}
	if !satExist {
		if _, err := harbor.CreateSatelliteProject(ctx); err != nil {
			return groups.NewSyncGroupBadGateway().WithPayload(appError("Error: creating satellite project", http.StatusBadGateway))
		}
	}

	if err := utils.CreateStateArtifact(ctx, &req); err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(appError("Error: Could not commit transaction", http.StatusInternalServerError))
	}
	committed = true

	return groups.NewSyncGroupOK().WithPayload(apiGroup(result))
}

func satelliteProjectsAndStates(ctx context.Context, q *database.Queries, satelliteID int32) ([]string, []string, error) {
	groupList, err := q.SatelliteGroupList(ctx, satelliteID)
	if err != nil {
		return nil, nil, err
	}

	var projects []string
	var groupStates []string
	for _, group := range groupList {
		grp, err := q.GetGroupByID(ctx, group.GroupID)
		if err != nil {
			return nil, nil, err
		}
		projects = append(projects, grp.Projects...)
		groupStates = append(groupStates, utils.AssembleGroupState(grp.GroupName))
	}

	return projects, groupStates, nil
}

func fetchSatelliteConfig(ctx context.Context, q *database.Queries, satelliteID int32) (database.Config, *swaggermodels.AppError) {
	satelliteConfig, err := q.SatelliteConfig(ctx, satelliteID)
	if err != nil {
		return database.Config{}, appError("Error: Failed to fetch satellite config", http.StatusInternalServerError)
	}

	configObject, err := q.GetConfigByID(ctx, satelliteConfig.ConfigID)
	if err != nil {
		return database.Config{}, appError("Error: Failed to fetch satellite config", http.StatusInternalServerError)
	}
	return configObject, nil
}

func stateArtifactFromAPI(in *swaggermodels.StateArtifact) gcmodels.StateArtifact {
	out := gcmodels.StateArtifact{
		Group:    in.Group,
		Registry: in.Registry,
	}
	if len(in.Artifacts) > 0 {
		out.Artifacts = make([]gcmodels.Artifact, 0, len(in.Artifacts))
	}
	for _, artifact := range in.Artifacts {
		if artifact == nil {
			continue
		}
		out.Artifacts = append(out.Artifacts, gcmodels.Artifact{
			Repository: artifact.Repository,
			Tag:        artifact.Tag,
			Labels:     artifact.Labels,
			Type:       artifact.Type,
			Digest:     artifact.Digest,
			Deleted:    artifact.Deleted,
		})
	}
	return out
}
