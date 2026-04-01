package server

import (
	"context"
	"log"
	"net/http"
	"time"

	openapierrors "github.com/go-openapi/errors"
	"github.com/go-openapi/loads"
	openapimiddleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"

	apimodels "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/models"
	apirest "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi"
	apiops "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations"
	apiauth "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/auth"
	apisystem "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/system"
	apiusers "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/users"
	internalmiddleware "github.com/container-registry/harbor-satellite/ground-control/internal/middleware"
)

type apiPrincipal struct {
	User         AuthUser
	SessionToken string
}

func (s *Server) newGeneratedAPIHandler() http.Handler {
	doc, err := loads.Analyzed(apirest.FlatSwaggerJSON, "")
	if err != nil {
		log.Fatalf("failed to load generated swagger spec: %v", err)
	}

	api := apiops.NewGroundcontrolAPI(doc)
	api.ServeError = serveGeneratedAPIError
	api.BearerAuthAuth = s.authenticateBearerPrincipal
	api.BasicAuthAuth = s.authenticateBasicPrincipal

	api.SystemPingHandler = apisystem.PingHandlerFunc(s.handleGeneratedPing)
	api.SystemGetHealthHandler = apisystem.GetHealthHandlerFunc(s.handleGeneratedHealth)
	api.AuthLoginHandler = apiauth.LoginHandlerFunc(s.handleGeneratedLogin)
	api.AuthLogoutHandler = apiauth.LogoutHandlerFunc(s.handleGeneratedLogout)
	api.UsersListUsersHandler = apiusers.ListUsersHandlerFunc(s.handleGeneratedListUsers)
	api.UsersGetUserHandler = apiusers.GetUserHandlerFunc(s.handleGeneratedGetUser)
	api.UsersCreateUserHandler = apiusers.CreateUserHandlerFunc(s.handleGeneratedCreateUser)
	api.UsersDeleteUserHandler = apiusers.DeleteUserHandlerFunc(s.handleGeneratedDeleteUser)
	api.UsersChangeOwnPasswordHandler = apiusers.ChangeOwnPasswordHandlerFunc(s.handleGeneratedChangeOwnPassword)
	api.UsersChangeUserPasswordHandler = apiusers.ChangeUserPasswordHandlerFunc(s.handleGeneratedChangeUserPassword)

	api.AddMiddlewareFor(http.MethodPost, "/login", internalmiddleware.RateLimitMiddleware(s.rateLimiter))

	return api.Serve(nil)
}

func serveGeneratedAPIError(rw http.ResponseWriter, _ *http.Request, err error) {
	statusCode, message := operationStatus(err)
	if statusCode == http.StatusInternalServerError {
		if codedErr, ok := err.(openapierrors.Error); ok {
			statusCode = int(codedErr.Code())
			message = err.Error()
		}
	}

	WriteJSONError(rw, message, statusCode)
}

func (s *Server) authenticateBearerPrincipal(token string) (any, error) {
	principal, err := s.authenticateBearer(context.Background(), token)
	if err != nil {
		return nil, openapierrors.Unauthenticated("BearerAuth")
	}

	return principal, nil
}

func (s *Server) authenticateBasicPrincipal(username, password string) (any, error) {
	principal, err := s.authenticateBasic(context.Background(), username, password)
	if err != nil {
		return nil, openapierrors.Unauthenticated("BasicAuth")
	}

	return principal, nil
}

func (s *Server) handleGeneratedPing(_ apisystem.PingParams) openapimiddleware.Responder {
	return apisystem.NewPingOK().WithPayload("pong")
}

func (s *Server) handleGeneratedHealth(_ apisystem.GetHealthParams) openapimiddleware.Responder {
	if err := s.db.Ping(); err != nil {
		log.Printf("error pinging db: %v", err)
		return apisystem.NewGetHealthServiceUnavailable().WithPayload(&apimodels.HealthResponse{Status: swag.String("unhealthy")})
	}

	return apisystem.NewGetHealthOK().WithPayload(&apimodels.HealthResponse{Status: swag.String("healthy")})
}

func (s *Server) handleGeneratedLogin(params apiauth.LoginParams) openapimiddleware.Responder {
	credentials := params.Credentials
	if credentials == nil || credentials.Username == nil || credentials.Password == nil {
		return apiauth.NewLoginBadRequest().WithPayload(newAPIError("Invalid request body"))
	}

	result, err := s.login(params.HTTPRequest.Context(), swag.StringValue(credentials.Username), string(*credentials.Password))
	if err != nil {
		return loginErrorResponder(err)
	}

	return apiauth.NewLoginOK().WithPayload(&apimodels.LoginResponse{
		Token:     swag.String(result.Token),
		ExpiresAt: dateTimePtr(result.ExpiresAt),
	})
}

func (s *Server) handleGeneratedLogout(params apiauth.LogoutParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok || p.SessionToken == "" {
		return apiauth.NewLogoutUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	if err := s.logout(params.HTTPRequest.Context(), p.SessionToken); err != nil {
		return logoutErrorResponder(err)
	}

	return apiauth.NewLogoutNoContent()
}

func (s *Server) handleGeneratedListUsers(params apiusers.ListUsersParams, principal any) openapimiddleware.Responder {
	if _, ok := principalFromAny(principal); !ok {
		return apiusers.NewListUsersUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	users, err := s.listUsers(params.HTTPRequest.Context())
	if err != nil {
		return listUsersErrorResponder(err)
	}

	response := make(apimodels.Users, 0, len(users))
	for _, user := range users {
		response = append(response, newAPIUser(user))
	}

	return apiusers.NewListUsersOK().WithPayload(response)
}

func (s *Server) handleGeneratedGetUser(params apiusers.GetUserParams, principal any) openapimiddleware.Responder {
	if _, ok := principalFromAny(principal); !ok {
		return apiusers.NewGetUserUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	user, err := s.getUser(params.HTTPRequest.Context(), params.Username)
	if err != nil {
		return getUserErrorResponder(err)
	}

	return apiusers.NewGetUserOK().WithPayload(newAPIUser(user))
}

func (s *Server) handleGeneratedCreateUser(params apiusers.CreateUserParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok {
		return apiusers.NewCreateUserUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}
	if p.User.Role != roleSystemAdmin {
		return apiusers.NewCreateUserForbidden().WithPayload(newAPIError("Forbidden"))
	}

	request := params.User
	if request == nil || request.Username == nil || request.Password == nil {
		return apiusers.NewCreateUserBadRequest().WithPayload(newAPIError("Invalid request body"))
	}

	user, err := s.createUser(params.HTTPRequest.Context(), swag.StringValue(request.Username), string(*request.Password))
	if err != nil {
		return createUserErrorResponder(err)
	}

	return apiusers.NewCreateUserCreated().WithPayload(newAPIUser(user))
}

func (s *Server) handleGeneratedDeleteUser(params apiusers.DeleteUserParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok {
		return apiusers.NewDeleteUserUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}
	if p.User.Role != roleSystemAdmin {
		return apiusers.NewDeleteUserForbidden().WithPayload(newAPIError("Forbidden"))
	}

	if err := s.deleteUser(params.HTTPRequest.Context(), p.User, params.Username); err != nil {
		return deleteUserErrorResponder(err)
	}

	return apiusers.NewDeleteUserNoContent()
}

func (s *Server) handleGeneratedChangeOwnPassword(params apiusers.ChangeOwnPasswordParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok {
		return apiusers.NewChangeOwnPasswordUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	request := params.Password
	if request == nil || request.CurrentPassword == nil || request.NewPassword == nil {
		return apiusers.NewChangeOwnPasswordBadRequest().WithPayload(newAPIError("Invalid request body"))
	}

	err := s.changeOwnPassword(params.HTTPRequest.Context(), p.User, string(*request.CurrentPassword), string(*request.NewPassword))
	if err != nil {
		return changeOwnPasswordErrorResponder(err)
	}

	return apiusers.NewChangeOwnPasswordNoContent()
}

func (s *Server) handleGeneratedChangeUserPassword(params apiusers.ChangeUserPasswordParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok {
		return apiusers.NewChangeUserPasswordUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}
	if p.User.Role != roleSystemAdmin {
		return apiusers.NewChangeUserPasswordForbidden().WithPayload(newAPIError("Forbidden"))
	}

	request := params.Password
	if request == nil || request.NewPassword == nil {
		return apiusers.NewChangeUserPasswordBadRequest().WithPayload(newAPIError("Invalid request body"))
	}

	err := s.changeUserPassword(params.HTTPRequest.Context(), params.Username, string(*request.NewPassword))
	if err != nil {
		return changeUserPasswordErrorResponder(err)
	}

	return apiusers.NewChangeUserPasswordNoContent()
}

func principalFromAny(principal any) (apiPrincipal, bool) {
	p, ok := principal.(apiPrincipal)
	return p, ok
}

func newAPIError(message string) *apimodels.ErrorResponse {
	return &apimodels.ErrorResponse{Error: swag.String(message)}
}

func newAPIUser(user userView) *apimodels.User {
	return &apimodels.User{
		ID:        swag.Int32(user.ID),
		Username:  swag.String(user.Username),
		Role:      swag.String(user.Role),
		CreatedAt: dateTimePtr(user.CreatedAt),
	}
}

func dateTimePtr(value time.Time) *strfmt.DateTime {
	dateTime := strfmt.DateTime(value)
	return &dateTime
}

func loginErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	if statusCode == http.StatusBadRequest {
		return apiauth.NewLoginBadRequest().WithPayload(newAPIError(message))
	}
	if statusCode == http.StatusUnauthorized {
		return apiauth.NewLoginUnauthorized().WithPayload(newAPIError(message))
	}
	return apiauth.NewLoginInternalServerError().WithPayload(newAPIError(message))
}

func logoutErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	if statusCode == http.StatusUnauthorized {
		return apiauth.NewLogoutUnauthorized().WithPayload(newAPIError(message))
	}
	return apiauth.NewLogoutInternalServerError().WithPayload(newAPIError(message))
}

func listUsersErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	if statusCode == http.StatusUnauthorized {
		return apiusers.NewListUsersUnauthorized().WithPayload(newAPIError(message))
	}
	return apiusers.NewListUsersInternalServerError().WithPayload(newAPIError(message))
}

func getUserErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	if statusCode == http.StatusUnauthorized {
		return apiusers.NewGetUserUnauthorized().WithPayload(newAPIError(message))
	}
	if statusCode == http.StatusNotFound {
		return apiusers.NewGetUserNotFound().WithPayload(newAPIError(message))
	}
	return apiusers.NewGetUserInternalServerError().WithPayload(newAPIError(message))
}

func createUserErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	switch statusCode {
	case http.StatusBadRequest:
		return apiusers.NewCreateUserBadRequest().WithPayload(newAPIError(message))
	case http.StatusConflict:
		return apiusers.NewCreateUserConflict().WithPayload(newAPIError(message))
	default:
		return apiusers.NewCreateUserInternalServerError().WithPayload(newAPIError(message))
	}
}

func deleteUserErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	switch statusCode {
	case http.StatusBadRequest:
		return apiusers.NewDeleteUserBadRequest().WithPayload(newAPIError(message))
	case http.StatusNotFound:
		return apiusers.NewDeleteUserNotFound().WithPayload(newAPIError(message))
	default:
		return apiusers.NewDeleteUserInternalServerError().WithPayload(newAPIError(message))
	}
}

func changeOwnPasswordErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	if statusCode == http.StatusBadRequest {
		return apiusers.NewChangeOwnPasswordBadRequest().WithPayload(newAPIError(message))
	}
	if statusCode == http.StatusUnauthorized {
		return apiusers.NewChangeOwnPasswordUnauthorized().WithPayload(newAPIError(message))
	}
	return apiusers.NewChangeOwnPasswordInternalServerError().WithPayload(newAPIError(message))
}

func changeUserPasswordErrorResponder(err error) openapimiddleware.Responder {
	statusCode, message := operationStatus(err)
	switch statusCode {
	case http.StatusBadRequest:
		return apiusers.NewChangeUserPasswordBadRequest().WithPayload(newAPIError(message))
	case http.StatusNotFound:
		return apiusers.NewChangeUserPasswordNotFound().WithPayload(newAPIError(message))
	default:
		return apiusers.NewChangeUserPasswordInternalServerError().WithPayload(newAPIError(message))
	}
}
