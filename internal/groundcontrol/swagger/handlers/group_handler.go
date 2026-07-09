package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/groups"
	"github.com/go-openapi/runtime/middleware"
)

func AddSatelliteToGroup(params groups.AddSatelliteToGroupParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation groups.AddSatelliteToGroup has not yet been implemented")
}

func DeleteGroup(params groups.DeleteGroupParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation groups.DeleteGroup has not yet been implemented")
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
	_ = params
	_ = principal
	return notImplemented("operation groups.RemoveSatelliteFromGroup has not yet been implemented")
}

func SyncGroup(params groups.SyncGroupParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation groups.SyncGroup has not yet been implemented")
}
