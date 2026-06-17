// This file is safe to edit. Once it exists it will not be overwritten

package restapi

import (
	"crypto/tls"
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"

	"github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations"
	"github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/system"
	"github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/users"
)

//go:generate swagger generate server --target ../../generated --name Groundcontrol --spec ../../../../api/v1/swagger.yaml --principal any --exclude-main

func configureFlags(api *operations.GroundcontrolAPI) {
	// api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
	_ = api
}

func configureAPI(api *operations.GroundcontrolAPI) http.Handler {
	// configure the api here
	api.ServeError = errors.ServeError

	// Set your custom logger if needed. Default one is log.Printf
	// Expected interface func(string, ...any)
	//
	// Example:
	// api.Logger = log.Printf

	api.UseSwaggerUI()
	// To continue using redoc as your UI, uncomment the following line
	// api.UseRedoc()

	api.JSONConsumer = runtime.JSONConsumer()

	api.JSONProducer = runtime.JSONProducer()

	// Applies when the Authorization header is set with the Basic scheme
	if api.BasicAuthAuth == nil {
		api.BasicAuthAuth = func(user string, password string) (any, error) {
			_ = user
			_ = password

			return nil, errors.NotImplemented("basic auth  (BasicAuth) has not yet been implemented")
		}
	}
	// Applies when the "Authorization" header is set
	if api.BearerAuthAuth == nil {
		api.BearerAuthAuth = func(token string) (any, error) {
			_ = token

			return nil, errors.NotImplemented("api key auth (BearerAuth) Authorization from header param [Authorization] has not yet been implemented")
		}
	}

	// Set your custom authorizer if needed. Default one is security.Authorized()
	// Expected interface runtime.Authorizer
	//
	// Example:
	// api.APIAuthorizer = security.Authorized()

	if api.UsersChangeOwnPasswordHandler == nil {
		api.UsersChangeOwnPasswordHandler = users.ChangeOwnPasswordHandlerFunc(func(params users.ChangeOwnPasswordParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation users.ChangeOwnPassword has not yet been implemented")
		})
	}
	if api.UsersChangeUserPasswordHandler == nil {
		api.UsersChangeUserPasswordHandler = users.ChangeUserPasswordHandlerFunc(func(params users.ChangeUserPasswordParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation users.ChangeUserPassword has not yet been implemented")
		})
	}
	if api.UsersCreateUserHandler == nil {
		api.UsersCreateUserHandler = users.CreateUserHandlerFunc(func(params users.CreateUserParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation users.CreateUser has not yet been implemented")
		})
	}
	if api.UsersDeleteUserHandler == nil {
		api.UsersDeleteUserHandler = users.DeleteUserHandlerFunc(func(params users.DeleteUserParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation users.DeleteUser has not yet been implemented")
		})
	}
	if api.SystemGetHealthHandler == nil {
		api.SystemGetHealthHandler = system.GetHealthHandlerFunc(func(params system.GetHealthParams) middleware.Responder {
			_ = params

			return middleware.NotImplemented("operation system.GetHealth has not yet been implemented")
		})
	}
	if api.UsersGetUserHandler == nil {
		api.UsersGetUserHandler = users.GetUserHandlerFunc(func(params users.GetUserParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation users.GetUser has not yet been implemented")
		})
	}
	if api.UsersListUsersHandler == nil {
		api.UsersListUsersHandler = users.ListUsersHandlerFunc(func(params users.ListUsersParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation users.ListUsers has not yet been implemented")
		})
	}
	if api.AuthLoginHandler == nil {
		api.AuthLoginHandler = auth.LoginHandlerFunc(func(params auth.LoginParams) middleware.Responder {
			_ = params

			return middleware.NotImplemented("operation auth.Login has not yet been implemented")
		})
	}
	if api.AuthLogoutHandler == nil {
		api.AuthLogoutHandler = auth.LogoutHandlerFunc(func(params auth.LogoutParams, principal any) middleware.Responder {
			_ = params
			_ = principal

			return middleware.NotImplemented("operation auth.Logout has not yet been implemented")
		})
	}
	if api.SystemPingHandler == nil {
		api.SystemPingHandler = system.PingHandlerFunc(func(params system.PingParams) middleware.Responder {
			_ = params

			return middleware.NotImplemented("operation system.Ping has not yet been implemented")
		})
	}

	api.PreServerShutdown = func() {}

	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	// Make all necessary changes to the TLS configuration here.
	_ = tlsConfig
}

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix".
func configureServer(server *http.Server, scheme, addr string) {
	_ = server
	_ = scheme
	_ = addr
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.json document.
// The middleware executes after routing but before authentication, binding and validation.
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

// The middleware configuration happens before anything, this middleware also applies to serving the swagger.json document.
// So this is a good place to plug in a panic handling middleware, logging and metrics.
func setupGlobalMiddleware(handler http.Handler) http.Handler {
	return handler
}
