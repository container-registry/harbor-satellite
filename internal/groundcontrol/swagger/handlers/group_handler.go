package handlers

import (
	"context"
	"database/sql"
	"errors"
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
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
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
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to check satellite group membership", err))
	}
	if alreadyInGroup {
		return groups.NewAddSatelliteToGroupOK().WithPayload(&swaggermodels.APIMessageResponse{Message: "Satellite is already in the group"})
	}

	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to start group membership transaction", err))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	if err := q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{
		SatelliteID: sat.ID,
		GroupID:     grp.ID,
	}); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to add satellite to group", err))
	}

	projects, groupStates, err := satelliteProjectsAndStates(ctx, q, sat.ID)
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to load updated satellite group membership", err))
	}

	configObject, errPayload := fetchSatelliteConfig(ctx, q, sat.ID)
	if errPayload != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(errPayload)
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(ctx, sat.ID)
	if err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to load satellite robot account", err))
	}
	if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to update satellite robot permissions", err))
	}
	if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, configObject.ConfigName); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to update satellite state artifact", err))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewAddSatelliteToGroupInternalServerError().WithPayload(internalError("Failed to commit group membership transaction", err))
	}
	committed = true

	return groups.NewAddSatelliteToGroupOK().WithPayload(&swaggermodels.APIMessageResponse{Message: "Satellite successfully added to group"})
}

func DeleteGroup(params groups.DeleteGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
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
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to start group deletion transaction", err))
	}
	q := svc.queries.WithTx(tx)
	committed := false
	defer rollbackUnlessCommitted(tx, &committed)

	group, err := q.GetGroupByName(ctx, params.Group)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groups.NewDeleteGroupNotFound().WithPayload(appError("Error: Group Not Found", http.StatusNotFound))
		}
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to load group for deletion", err))
	}

	satellites, err := q.GroupSatelliteList(ctx, group.ID)
	if err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to list satellites attached to group", err))
	}

	for _, satellite := range satellites {
		if err := q.RemoveSatelliteFromGroup(ctx, database.RemoveSatelliteFromGroupParams{
			SatelliteID: satellite.SatelliteID,
			GroupID:     group.ID,
		}); err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to detach satellite from group", err))
		}

		robotAcc, err := q.GetRobotAccBySatelliteID(ctx, satellite.SatelliteID)
		if err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to load satellite robot account", err))
		}

		projects, groupStates, err := satelliteProjectsAndStates(ctx, q, satellite.SatelliteID)
		if err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to load remaining satellite groups", err))
		}

		if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to update satellite robot permissions", err))
		}

		sat, err := q.GetSatellite(ctx, satellite.SatelliteID)
		if err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to load satellite while deleting group", err))
		}

		configObject, errPayload := fetchSatelliteConfig(ctx, q, sat.ID)
		if errPayload != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(errPayload)
		}

		if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, configObject.ConfigName); err != nil {
			return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to update satellite state artifact", err))
		}
	}

	if err := q.DeleteGroup(ctx, group.ID); err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to delete group", err))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Failed to commit group deletion transaction", err))
	}
	committed = true

	if err := utils.DeleteArtifact(utils.ConstructHarborDeleteURL(params.Group, "group")); err != nil {
		return groups.NewDeleteGroupInternalServerError().WithPayload(internalError("Group was deleted, but its Harbor state artifact could not be removed", err))
	}

	return groups.NewDeleteGroupOK().WithPayload(map[string]string{})
}

func GetGroup(params groups.GetGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewGetGroupInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewGetGroupUnauthorized().WithPayload(errPayload)
	}

	group, err := svc.queries.GetGroupByName(params.HTTPRequest.Context(), params.Group)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groups.NewGetGroupNotFound().WithPayload(appError("Group not found", http.StatusNotFound))
		}
		return groups.NewGetGroupInternalServerError().WithPayload(internalError("Failed to load group", err))
	}

	return groups.NewGetGroupOK().WithPayload(apiGroup(group))
}

func ListGroupSatellites(params groups.ListGroupSatellitesParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewListGroupSatellitesInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewListGroupSatellitesUnauthorized().WithPayload(errPayload)
	}
	if _, err := svc.queries.GetGroupByName(params.HTTPRequest.Context(), params.Group); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groups.NewListGroupSatellitesNotFound().WithPayload(appError("Group not found", http.StatusNotFound))
		}
		return groups.NewListGroupSatellitesInternalServerError().WithPayload(internalError("Failed to load group before listing satellites", err))
	}

	rows, err := svc.queries.GetSatellitesByGroupName(params.HTTPRequest.Context(), params.Group)
	if err != nil {
		return groups.NewListGroupSatellitesInternalServerError().WithPayload(internalError("Failed to list satellites attached to group", err))
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
		return groups.NewListGroupsInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewListGroupsUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.ListGroups(params.HTTPRequest.Context())
	if err != nil {
		return groups.NewListGroupsInternalServerError().WithPayload(internalError("Failed to list groups", err))
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
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
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
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to start group membership transaction", err))
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
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to remove satellite from group", err))
	}

	robotAcc, err := q.GetRobotAccBySatelliteID(ctx, sat.ID)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to load satellite robot account", err))
	}

	projects, groupStates, err := satelliteProjectsAndStates(ctx, q, sat.ID)
	if err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to load remaining satellite groups", err))
	}

	if _, err := utils.UpdateRobotProjects(ctx, projects, robotAcc.RobotID); err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to update satellite robot permissions", err))
	}

	configObject, errPayload := fetchSatelliteConfig(ctx, q, sat.ID)
	if errPayload != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(errPayload)
	}

	if err := utils.CreateOrUpdateSatStateArtifact(ctx, sat.Name, groupStates, configObject.ConfigName); err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to update satellite state artifact", err))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewRemoveSatelliteFromGroupInternalServerError().WithPayload(internalError("Failed to commit group membership transaction", err))
	}
	committed = true

	return groups.NewRemoveSatelliteFromGroupOK().WithPayload(map[string]string{})
}

func SyncGroup(params groups.SyncGroupParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to initialize group service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return groups.NewSyncGroupUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil {
		return groups.NewSyncGroupBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}
	if !utils.IsValidName(params.Body.Group) {
		return groups.NewSyncGroupBadRequest().WithPayload(appError("Invalid group name: must be 1-255 chars, start with letter/number, and contain only lowercase letters, numbers, and ._-", http.StatusBadRequest))
	}

	req := stateArtifactFromAPI(params.Body)
	ctx := params.HTTPRequest.Context()
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to start group synchronization transaction", err))
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
		return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to create or update group record", err))
	}

	satellites, err := q.GroupSatelliteList(ctx, result.ID)
	if err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to list satellites attached to synchronized group", err))
	}

	for _, satellite := range satellites {
		robotAcc, err := q.GetRobotAccBySatelliteID(ctx, satellite.SatelliteID)
		if err != nil {
			return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to load satellite robot account while synchronizing group", err))
		}
		satelliteProjects, _, err := satelliteProjectsAndStates(ctx, q, satellite.SatelliteID)
		if err != nil {
			return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to load satellite projects while synchronizing group", err))
		}
		if _, err := utils.UpdateRobotProjects(ctx, satelliteProjects, robotAcc.RobotID); err != nil {
			return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to update satellite robot permissions while synchronizing group", err))
		}
	}

	satExist, err := harbor.GetProject(ctx, "satellite")
	if err != nil {
		return groups.NewSyncGroupBadGateway().WithPayload(upstreamError("Failed to check the Harbor satellite project", err))
	}
	if !satExist {
		if _, err := harbor.CreateSatelliteProject(ctx); err != nil {
			return groups.NewSyncGroupBadGateway().WithPayload(upstreamError("Failed to create the Harbor satellite project", err))
		}
	}

	if err := utils.CreateStateArtifact(ctx, &req); err != nil {
		return groups.NewSyncGroupBadGateway().WithPayload(upstreamError("Failed to create or push group state artifact in Harbor", err))
	}

	if err := tx.Commit(); err != nil {
		return groups.NewSyncGroupInternalServerError().WithPayload(internalError("Failed to commit group synchronization transaction", err))
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
		return database.Config{}, internalError("Failed to load satellite configuration assignment", err)
	}

	configObject, err := q.GetConfigByID(ctx, satelliteConfig.ConfigID)
	if err != nil {
		return database.Config{}, internalError("Failed to load assigned satellite configuration", err)
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
