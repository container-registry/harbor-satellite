package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	openapierrors "github.com/go-openapi/errors"
	"github.com/go-openapi/loads"
	openapimiddleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/lib/pq"

	apimodels "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/models"
	apirest "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi"
	apiops "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations"
	apiauth "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/auth"
	apisystem "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/system"
	apiusers "github.com/container-registry/harbor-satellite/ground-control/internal/api/generated/restapi/operations/users"
	"github.com/container-registry/harbor-satellite/ground-control/internal/auth"
	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
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
	statusCode := http.StatusInternalServerError
	if codedErr, ok := err.(openapierrors.Error); ok {
		statusCode = int(codedErr.Code())
	}

	WriteJSONError(rw, err.Error(), statusCode)
}

func (s *Server) authenticateBearerPrincipal(token string) (any, error) {
	session, err := s.dbQueries.GetSessionByToken(context.Background(), token)
	if err != nil {
		return nil, openapierrors.Unauthenticated("BearerAuth")
	}

	return apiPrincipal{
		User: AuthUser{
			ID:       session.UserID,
			Username: session.Username,
			Role:     session.Role,
		},
		SessionToken: token,
	}, nil
}

func (s *Server) authenticateBasicPrincipal(username, password string) (any, error) {
	user, err := s.dbQueries.GetUserByUsername(context.Background(), username)
	if err != nil {
		return nil, openapierrors.Unauthenticated("BasicAuth")
	}

	if !auth.VerifyPassword(password, user.PasswordHash) {
		return nil, openapierrors.Unauthenticated("BasicAuth")
	}

	return apiPrincipal{
		User: AuthUser{
			ID:       user.ID,
			Username: user.Username,
			Role:     user.Role,
		},
	}, nil
}

func (s *Server) handleGeneratedPing(_ apisystem.PingParams) openapimiddleware.Responder {
	return apisystem.NewPingOK().WithPayload("pong")
}

func (s *Server) handleGeneratedHealth(_ apisystem.GetHealthParams) openapimiddleware.Responder {
	if err := s.db.Ping(); err != nil {
		log.Printf("error pinging db: %v", err)
		return apisystem.NewGetHealthServiceUnavailable().WithPayload(&apimodels.HealthResponse{
			Status: swag.String("unhealthy"),
		})
	}

	return apisystem.NewGetHealthOK().WithPayload(&apimodels.HealthResponse{
		Status: swag.String("healthy"),
	})
}

func (s *Server) handleGeneratedLogin(params apiauth.LoginParams) openapimiddleware.Responder {
	credentials := params.Credentials
	if credentials == nil || credentials.Username == nil || credentials.Password == nil {
		return apiauth.NewLoginBadRequest().WithPayload(newAPIError("Invalid request body"))
	}

	username := strings.TrimSpace(swag.StringValue(credentials.Username))
	password := string(*credentials.Password)
	if username == "" || password == "" {
		return apiauth.NewLoginUnauthorized().WithPayload(newAPIError("Invalid credentials"))
	}

	attempts, err := s.dbQueries.GetLoginAttempts(params.HTTPRequest.Context(), username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return apiauth.NewLoginInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		return apiauth.NewLoginUnauthorized().WithPayload(newAPIError("Invalid credentials"))
	}

	user, err := s.dbQueries.GetUserByUsername(params.HTTPRequest.Context(), username)
	if err != nil {
		s.recordFailedAttempt(params.HTTPRequest, username)
		return apiauth.NewLoginUnauthorized().WithPayload(newAPIError("Invalid credentials"))
	}

	if !auth.VerifyPassword(password, user.PasswordHash) {
		s.recordFailedAttempt(params.HTTPRequest, username)
		return apiauth.NewLoginUnauthorized().WithPayload(newAPIError("Invalid credentials"))
	}

	_ = s.dbQueries.ResetLoginAttempts(params.HTTPRequest.Context(), username)

	token, err := auth.GenerateSessionToken()
	if err != nil {
		return apiauth.NewLoginInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	expiresAt := time.Now().Add(s.sessionDuration)
	_, err = s.dbQueries.CreateSession(params.HTTPRequest.Context(), database.CreateSessionParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return apiauth.NewLoginInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	return apiauth.NewLoginOK().WithPayload(&apimodels.LoginResponse{
		Token:     swag.String(token),
		ExpiresAt: dateTimePtr(expiresAt),
	})
}

func (s *Server) handleGeneratedLogout(params apiauth.LogoutParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok || p.SessionToken == "" {
		return apiauth.NewLogoutUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	if err := s.dbQueries.DeleteSession(params.HTTPRequest.Context(), p.SessionToken); err != nil {
		return apiauth.NewLogoutInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	return apiauth.NewLogoutNoContent()
}

func (s *Server) handleGeneratedListUsers(params apiusers.ListUsersParams, principal any) openapimiddleware.Responder {
	if _, ok := principalFromAny(principal); !ok {
		return apiusers.NewListUsersUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	users, err := s.dbQueries.ListUsers(params.HTTPRequest.Context())
	if err != nil {
		return apiusers.NewListUsersInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	response := make(apimodels.Users, 0, len(users))
	for _, user := range users {
		response = append(response, newAPIUser(user.ID, user.Username, user.Role, user.CreatedAt))
	}

	return apiusers.NewListUsersOK().WithPayload(response)
}

func (s *Server) handleGeneratedGetUser(params apiusers.GetUserParams, principal any) openapimiddleware.Responder {
	if _, ok := principalFromAny(principal); !ok {
		return apiusers.NewGetUserUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}

	user, err := s.dbQueries.GetUserByUsername(params.HTTPRequest.Context(), params.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiusers.NewGetUserNotFound().WithPayload(newAPIError("User not found"))
		}
		return apiusers.NewGetUserInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if user.Role == roleSystemAdmin {
		return apiusers.NewGetUserNotFound().WithPayload(newAPIError("User not found"))
	}

	return apiusers.NewGetUserOK().WithPayload(newAPIUser(user.ID, user.Username, user.Role, user.CreatedAt))
}

func (s *Server) handleGeneratedCreateUser(params apiusers.CreateUserParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok {
		return apiusers.NewCreateUserUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}
	if p.User.Role != roleSystemAdmin {
		return apiusers.NewCreateUserForbidden().WithPayload(newAPIError("Forbidden"))
	}

	userParams := params.User
	if userParams == nil || userParams.Username == nil || userParams.Password == nil {
		return apiusers.NewCreateUserBadRequest().WithPayload(newAPIError("Invalid request body"))
	}

	username := strings.TrimSpace(swag.StringValue(userParams.Username))
	if username == "" {
		return apiusers.NewCreateUserBadRequest().WithPayload(newAPIError("Username is required"))
	}
	if username == "admin" {
		return apiusers.NewCreateUserBadRequest().WithPayload(newAPIError("Username 'admin' is reserved"))
	}

	password := string(*userParams.Password)
	if err := s.passwordPolicy.Validate(password); err != nil {
		return apiusers.NewCreateUserBadRequest().WithPayload(newAPIError(err.Error()))
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return apiusers.NewCreateUserInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	user, err := s.dbQueries.CreateUser(params.HTTPRequest.Context(), database.CreateUserParams{
		Username:     username,
		PasswordHash: hash,
		Role:         roleAdmin,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apiusers.NewCreateUserConflict().WithPayload(newAPIError("User already exists"))
		}
		return apiusers.NewCreateUserInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	return apiusers.NewCreateUserCreated().WithPayload(newAPIUser(user.ID, user.Username, user.Role, user.CreatedAt))
}

func (s *Server) handleGeneratedDeleteUser(params apiusers.DeleteUserParams, principal any) openapimiddleware.Responder {
	p, ok := principalFromAny(principal)
	if !ok {
		return apiusers.NewDeleteUserUnauthorized().WithPayload(newAPIError("Unauthorized"))
	}
	if p.User.Role != roleSystemAdmin {
		return apiusers.NewDeleteUserForbidden().WithPayload(newAPIError("Forbidden"))
	}

	username := params.Username
	if username == p.User.Username {
		return apiusers.NewDeleteUserBadRequest().WithPayload(newAPIError("Cannot delete yourself"))
	}
	if username == "admin" {
		return apiusers.NewDeleteUserBadRequest().WithPayload(newAPIError("Cannot delete system admin"))
	}

	user, err := s.dbQueries.GetUserByUsername(params.HTTPRequest.Context(), username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiusers.NewDeleteUserNotFound().WithPayload(newAPIError("User not found"))
		}
		return apiusers.NewDeleteUserInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if err := s.dbQueries.DeleteUserSessions(params.HTTPRequest.Context(), user.ID); err != nil {
		return apiusers.NewDeleteUserInternalServerError().WithPayload(newAPIError("Internal server error"))
	}
	if err := s.dbQueries.DeleteUser(params.HTTPRequest.Context(), username); err != nil {
		return apiusers.NewDeleteUserInternalServerError().WithPayload(newAPIError("Internal server error"))
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

	newPassword := string(*request.NewPassword)
	if err := s.passwordPolicy.Validate(newPassword); err != nil {
		return apiusers.NewChangeOwnPasswordBadRequest().WithPayload(newAPIError(err.Error()))
	}

	user, err := s.dbQueries.GetUserByUsername(params.HTTPRequest.Context(), p.User.Username)
	if err != nil {
		return apiusers.NewChangeOwnPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if !auth.VerifyPassword(string(*request.CurrentPassword), user.PasswordHash) {
		return apiusers.NewChangeOwnPasswordUnauthorized().WithPayload(newAPIError("Current password is incorrect"))
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return apiusers.NewChangeOwnPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if err := s.dbQueries.UpdateUserPassword(params.HTTPRequest.Context(), database.UpdateUserPasswordParams{
		Username:     p.User.Username,
		PasswordHash: hash,
	}); err != nil {
		return apiusers.NewChangeOwnPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if err := s.dbQueries.DeleteUserSessions(params.HTTPRequest.Context(), user.ID); err != nil {
		return apiusers.NewChangeOwnPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
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

	newPassword := string(*request.NewPassword)
	if err := s.passwordPolicy.Validate(newPassword); err != nil {
		return apiusers.NewChangeUserPasswordBadRequest().WithPayload(newAPIError(err.Error()))
	}

	user, err := s.dbQueries.GetUserByUsername(params.HTTPRequest.Context(), params.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiusers.NewChangeUserPasswordNotFound().WithPayload(newAPIError("User not found"))
		}
		return apiusers.NewChangeUserPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return apiusers.NewChangeUserPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if err := s.dbQueries.UpdateUserPassword(params.HTTPRequest.Context(), database.UpdateUserPasswordParams{
		Username:     params.Username,
		PasswordHash: hash,
	}); err != nil {
		return apiusers.NewChangeUserPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
	}

	if err := s.dbQueries.DeleteUserSessions(params.HTTPRequest.Context(), user.ID); err != nil {
		return apiusers.NewChangeUserPasswordInternalServerError().WithPayload(newAPIError("Internal server error"))
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

func newAPIUser(id int32, username, role string, createdAt time.Time) *apimodels.User {
	return &apimodels.User{
		ID:        swag.Int32(id),
		Username:  swag.String(username),
		Role:      swag.String(role),
		CreatedAt: dateTimePtr(createdAt),
	}
}

func dateTimePtr(value time.Time) *strfmt.DateTime {
	dateTime := strfmt.DateTime(value)
	return &dateTime
}
