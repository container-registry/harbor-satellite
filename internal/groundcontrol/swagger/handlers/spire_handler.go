package handlers

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/spire"
	"github.com/go-openapi/runtime/middleware"
)

func GetSpireStatus(params spire.GetSpireStatusParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation spire.GetSpireStatus has not yet been implemented")
}

func ListSpireAgents(params spire.ListSpireAgentsParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation spire.ListSpireAgents has not yet been implemented")
}

func RegisterSatelliteWithSpiffe(params spire.RegisterSatelliteWithSpiffeParams, principal any) middleware.Responder {
	_ = params
	_ = principal
	return notImplemented("operation spire.RegisterSatelliteWithSpiffe has not yet been implemented")
}
