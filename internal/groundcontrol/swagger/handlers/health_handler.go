package handlers

import (
	"net/http"

	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/health"
	"github.com/go-openapi/runtime/middleware"
)

func Health(params health.HealthParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return health.NewHealthServiceUnavailable().WithPayload(&swaggermodels.APIHealthResponse{Status: "unhealthy"})
	}
	if err := svc.db.PingContext(params.HTTPRequest.Context()); err != nil {
		return health.NewHealthServiceUnavailable().WithPayload(&swaggermodels.APIHealthResponse{Status: "unhealthy"})
	}
	return health.NewHealthOK().WithPayload(&swaggermodels.APIHealthResponse{Status: "healthy"})
}

func Ping(params health.PingParams) middleware.Responder {
	_ = params
	return health.NewPingOK().WithPayload("pong")
}

func notImplemented(message string) middleware.Responder {
	return middleware.Error(http.StatusNotImplemented, message)
}
