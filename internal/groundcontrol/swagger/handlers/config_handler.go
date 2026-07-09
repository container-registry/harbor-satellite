package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/configs"
	"github.com/go-openapi/runtime/middleware"
)

func CreateConfig(params configs.CreateConfigParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation configs.CreateConfig has not yet been implemented")
}

func DeleteConfig(params configs.DeleteConfigParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation configs.DeleteConfig has not yet been implemented")
}

func GetConfig(params configs.GetConfigParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewGetConfigInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return configs.NewGetConfigUnauthorized().WithPayload(errPayload)
	}

	config, err := svc.queries.GetConfigByName(params.HTTPRequest.Context(), params.Config)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return configs.NewGetConfigNotFound().WithPayload(appError("Config not found", http.StatusNotFound))
		}
		return configs.NewGetConfigInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	return configs.NewGetConfigOK().WithPayload(databaseConfig(config))
}

func ListConfigs(params configs.ListConfigsParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return configs.NewListConfigsInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return configs.NewListConfigsUnauthorized().WithPayload(errPayload)
	}

	rows, err := svc.queries.ListConfigs(params.HTTPRequest.Context())
	if err != nil {
		return configs.NewListConfigsInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	response := make([]*swaggermodels.APIDatabaseConfig, 0, len(rows))
	for _, row := range rows {
		response = append(response, databaseConfig(row))
	}

	return configs.NewListConfigsOK().WithPayload(response)
}

func SetSatelliteConfig(params configs.SetSatelliteConfigParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation configs.SetSatelliteConfig has not yet been implemented")
}

func UpdateConfig(params configs.UpdateConfigParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation configs.UpdateConfig has not yet been implemented")
}
