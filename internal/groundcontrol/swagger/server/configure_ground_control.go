// This file is safe to edit. Once it exists it will not be overwritten

package server

import (
	"crypto/tls"
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"

	swaggerhandlers "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/handlers"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/configs"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/groups"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/health"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/satellites"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/spire"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/users"
)

//go:generate swagger generate server --target ../../../../../feat-srv --name GroundControl --spec ../../../../api/ground-control/swagger.yaml --model-package ./internal/groundcontrol/swagger/models --server-package ./internal/groundcontrol/swagger/server --principal any

func configureFlags(api *operations.GroundControlAPI) {
	// api.CommandLineOptionsGroups = []cmdutils.CommandLineOptionsGroup{ ... }
	_ = api
}

func configureAPI(api *operations.GroundControlAPI) http.Handler {
	api.ServeError = errors.ServeError
	api.UseSwaggerUI()

	api.JSONConsumer = runtime.JSONConsumer()
	api.JSONProducer = runtime.JSONProducer()
	api.TxtProducer = runtime.TextProducer()

	api.BearerAuthAuth = swaggerhandlers.AuthenticateBearer

	api.GroupsAddSatelliteToGroupHandler = groups.AddSatelliteToGroupHandlerFunc(swaggerhandlers.AddSatelliteToGroup)
	api.UsersChangeOwnPasswordHandler = users.ChangeOwnPasswordHandlerFunc(swaggerhandlers.ChangeOwnPassword)
	api.UsersChangeUserPasswordHandler = users.ChangeUserPasswordHandlerFunc(swaggerhandlers.ChangeUserPassword)
	api.ConfigsCreateConfigHandler = configs.CreateConfigHandlerFunc(swaggerhandlers.CreateConfig)
	api.UsersCreateUserHandler = users.CreateUserHandlerFunc(swaggerhandlers.CreateUser)
	api.ConfigsDeleteConfigHandler = configs.DeleteConfigHandlerFunc(swaggerhandlers.DeleteConfig)
	api.GroupsDeleteGroupHandler = groups.DeleteGroupHandlerFunc(swaggerhandlers.DeleteGroup)
	api.SatellitesDeleteSatelliteHandler = satellites.DeleteSatelliteHandlerFunc(swaggerhandlers.DeleteSatellite)
	api.UsersDeleteUserHandler = users.DeleteUserHandlerFunc(swaggerhandlers.DeleteUser)
	api.SatellitesGetCachedImagesHandler = satellites.GetCachedImagesHandlerFunc(swaggerhandlers.GetCachedImages)
	api.ConfigsGetConfigHandler = configs.GetConfigHandlerFunc(swaggerhandlers.GetConfig)
	api.GroupsGetGroupHandler = groups.GetGroupHandlerFunc(swaggerhandlers.GetGroup)
	api.SatellitesGetSatelliteHandler = satellites.GetSatelliteHandlerFunc(swaggerhandlers.GetSatellite)
	api.SatellitesGetSatelliteStatusHandler = satellites.GetSatelliteStatusHandlerFunc(swaggerhandlers.GetSatelliteStatus)
	api.SpireGetSpireStatusHandler = spire.GetSpireStatusHandlerFunc(swaggerhandlers.GetSpireStatus)
	api.UsersGetUserHandler = users.GetUserHandlerFunc(swaggerhandlers.GetUser)
	api.HealthHealthHandler = health.HealthHandlerFunc(swaggerhandlers.Health)
	api.SatellitesListActiveSatellitesHandler = satellites.ListActiveSatellitesHandlerFunc(swaggerhandlers.ListActiveSatellites)
	api.ConfigsListConfigsHandler = configs.ListConfigsHandlerFunc(swaggerhandlers.ListConfigs)
	api.GroupsListGroupSatellitesHandler = groups.ListGroupSatellitesHandlerFunc(swaggerhandlers.ListGroupSatellites)
	api.GroupsListGroupsHandler = groups.ListGroupsHandlerFunc(swaggerhandlers.ListGroups)
	api.SatellitesListSatellitesHandler = satellites.ListSatellitesHandlerFunc(swaggerhandlers.ListSatellites)
	api.SpireListSpireAgentsHandler = spire.ListSpireAgentsHandlerFunc(swaggerhandlers.ListSpireAgents)
	api.SatellitesListStaleSatellitesHandler = satellites.ListStaleSatellitesHandlerFunc(swaggerhandlers.ListStaleSatellites)
	api.UsersListUsersHandler = users.ListUsersHandlerFunc(swaggerhandlers.ListUsers)
	api.AuthLoginHandler = auth.LoginHandlerFunc(swaggerhandlers.Login)
	api.AuthLogoutHandler = auth.LogoutHandlerFunc(swaggerhandlers.Logout)
	api.HealthPingHandler = health.PingHandlerFunc(swaggerhandlers.Ping)
	api.SatellitesRegisterSatelliteHandler = satellites.RegisterSatelliteHandlerFunc(swaggerhandlers.RegisterSatellite)
	api.SpireRegisterSatelliteWithSpiffeHandler = spire.RegisterSatelliteWithSpiffeHandlerFunc(swaggerhandlers.RegisterSatelliteWithSpiffe)
	api.GroupsRemoveSatelliteFromGroupHandler = groups.RemoveSatelliteFromGroupHandlerFunc(swaggerhandlers.RemoveSatelliteFromGroup)
	api.ConfigsSetSatelliteConfigHandler = configs.SetSatelliteConfigHandlerFunc(swaggerhandlers.SetSatelliteConfig)
	api.SatellitesSpiffeZtrHandler = satellites.SpiffeZtrHandlerFunc(swaggerhandlers.SpiffeZtr)
	api.GroupsSyncGroupHandler = groups.SyncGroupHandlerFunc(swaggerhandlers.SyncGroup)
	api.SatellitesSyncSatelliteHandler = satellites.SyncSatelliteHandlerFunc(swaggerhandlers.SyncSatellite)
	api.ConfigsUpdateConfigHandler = configs.UpdateConfigHandlerFunc(swaggerhandlers.UpdateConfig)
	api.SatellitesZtrHandler = satellites.ZtrHandlerFunc(swaggerhandlers.Ztr)

	api.PreServerShutdown = func() {}
	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

func configureTLS(tlsConfig *tls.Config) {
	_ = tlsConfig
}

func configureServer(server *http.Server, scheme, addr string) {
	_ = server
	_ = scheme
	_ = addr
}

func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

func setupGlobalMiddleware(handler http.Handler) http.Handler {
	return handler
}
