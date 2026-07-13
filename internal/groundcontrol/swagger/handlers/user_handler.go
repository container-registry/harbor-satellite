package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/users"
	"github.com/go-openapi/runtime/middleware"
	"github.com/lib/pq"
)

func CreateUser(params users.CreateUserParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return users.NewCreateUserInternalServerError().WithPayload(internalError("Failed to initialize user service", err))
	}
	actor, errPayload := requireSystemAdmin(principal)
	if errPayload != nil {
		if errPayload.Code == http.StatusUnauthorized {
			return users.NewCreateUserUnauthorized().WithPayload(errPayload)
		}
		return users.NewCreateUserForbidden().WithPayload(errPayload)
	}
	if params.Body == nil || params.Body.Username == nil || params.Body.Password == nil {
		return users.NewCreateUserBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	username := *params.Body.Username
	password := string(*params.Body.Password)
	if username == "" {
		return users.NewCreateUserBadRequest().WithPayload(appError("Username is required", http.StatusBadRequest))
	}
	if username == roleAdmin {
		return users.NewCreateUserBadRequest().WithPayload(appError("Username 'admin' is reserved", http.StatusBadRequest))
	}
	if err := svc.passwordPolicy.Validate(password); err != nil {
		return users.NewCreateUserBadRequest().WithPayload(appError(err.Error(), http.StatusBadRequest))
	}

	hash, err := gcauth.HashPassword(password)
	if err != nil {
		return users.NewCreateUserInternalServerError().WithPayload(internalError("Failed to hash user password", err))
	}

	user, err := svc.queries.CreateUser(params.HTTPRequest.Context(), database.CreateUserParams{
		Username:     username,
		PasswordHash: hash,
		Role:         roleAdmin,
	})
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return users.NewCreateUserConflict().WithPayload(appError("User already exists", http.StatusConflict))
		}
		return users.NewCreateUserInternalServerError().WithPayload(internalError("Failed to create user", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpCreate,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     user.Username,
		Details:      map[string]any{"role": user.Role},
	})

	return users.NewCreateUserCreated().WithPayload(userResponse(user.ID, user.Username, user.Role, user.CreatedAt.Format("2006-01-02T15:04:05Z07:00")))
}

func ListUsers(params users.ListUsersParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return users.NewListUsersInternalServerError().WithPayload(internalError("Failed to initialize user service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return users.NewListUsersUnauthorized().WithPayload(errPayload)
	}

	dbUsers, err := svc.queries.ListUsers(params.HTTPRequest.Context())
	if err != nil {
		return users.NewListUsersInternalServerError().WithPayload(internalError("Failed to list users", err))
	}

	response := make([]*swaggermodels.UserResponse, 0, len(dbUsers))
	for _, user := range dbUsers {
		response = append(response, userResponse(user.ID, user.Username, user.Role, user.CreatedAt.Format("2006-01-02T15:04:05Z07:00")))
	}

	return users.NewListUsersOK().WithPayload(response)
}

func GetUser(params users.GetUserParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return users.NewGetUserInternalServerError().WithPayload(internalError("Failed to initialize user service", err))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return users.NewGetUserUnauthorized().WithPayload(errPayload)
	}

	user, err := svc.queries.GetUserByUsername(params.HTTPRequest.Context(), params.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return users.NewGetUserNotFound().WithPayload(appError("User not found", http.StatusNotFound))
		}
		return users.NewGetUserInternalServerError().WithPayload(internalError("Failed to load user", err))
	}
	if user.Role == roleSystemAdmin {
		return users.NewGetUserNotFound().WithPayload(appError("User not found", http.StatusNotFound))
	}

	return users.NewGetUserOK().WithPayload(userResponse(user.ID, user.Username, user.Role, user.CreatedAt.Format("2006-01-02T15:04:05Z07:00")))
}

func DeleteUser(params users.DeleteUserParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return users.NewDeleteUserInternalServerError().WithPayload(internalError("Failed to initialize user service", err))
	}
	currentUser, errPayload := requireSystemAdmin(principal)
	if errPayload != nil {
		if errPayload.Code == http.StatusUnauthorized {
			return users.NewDeleteUserUnauthorized().WithPayload(errPayload)
		}
		return users.NewDeleteUserForbidden().WithPayload(errPayload)
	}

	if params.Username == currentUser.Username {
		return users.NewDeleteUserBadRequest().WithPayload(appError("Cannot delete yourself", http.StatusBadRequest))
	}
	if params.Username == roleAdmin {
		return users.NewDeleteUserBadRequest().WithPayload(appError("Cannot delete system admin", http.StatusBadRequest))
	}

	user, err := svc.queries.GetUserByUsername(params.HTTPRequest.Context(), params.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return users.NewDeleteUserNotFound().WithPayload(appError("User not found", http.StatusNotFound))
		}
		return users.NewDeleteUserInternalServerError().WithPayload(internalError("Failed to load user for deletion", err))
	}

	if err := svc.queries.DeleteUserSessions(params.HTTPRequest.Context(), user.ID); err != nil {
		return users.NewDeleteUserInternalServerError().WithPayload(internalError("Failed to delete user sessions", err))
	}
	if err := svc.queries.DeleteUser(params.HTTPRequest.Context(), params.Username); err != nil {
		return users.NewDeleteUserInternalServerError().WithPayload(internalError("Failed to delete user", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpDelete,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        currentUser.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     params.Username,
	})

	return users.NewDeleteUserNoContent()
}

func ChangeOwnPassword(params users.ChangeOwnPasswordParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return users.NewChangeOwnPasswordInternalServerError().WithPayload(internalError("Failed to initialize user service", err))
	}
	currentUser, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return users.NewChangeOwnPasswordUnauthorized().WithPayload(errPayload)
	}
	if params.Body == nil || params.Body.CurrentPassword == nil || params.Body.NewPassword == nil {
		return users.NewChangeOwnPasswordBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	newPassword := string(*params.Body.NewPassword)
	if err := svc.passwordPolicy.Validate(newPassword); err != nil {
		return users.NewChangeOwnPasswordBadRequest().WithPayload(appError(err.Error(), http.StatusBadRequest))
	}

	user, err := svc.queries.GetUserByUsername(params.HTTPRequest.Context(), currentUser.Username)
	if err != nil {
		return users.NewChangeOwnPasswordInternalServerError().WithPayload(internalError("Failed to load current user", err))
	}
	if !gcauth.VerifyPassword(string(*params.Body.CurrentPassword), user.PasswordHash) {
		return users.NewChangeOwnPasswordUnauthorized().WithPayload(appError("Current password is incorrect", http.StatusUnauthorized))
	}

	hash, err := gcauth.HashPassword(newPassword)
	if err != nil {
		return users.NewChangeOwnPasswordInternalServerError().WithPayload(internalError("Failed to hash new password", err))
	}
	if err := svc.queries.UpdateUserPassword(params.HTTPRequest.Context(), database.UpdateUserPasswordParams{
		Username:     currentUser.Username,
		PasswordHash: hash,
	}); err != nil {
		return users.NewChangeOwnPasswordInternalServerError().WithPayload(internalError("Failed to update password", err))
	}
	if err := svc.queries.DeleteUserSessions(params.HTTPRequest.Context(), user.ID); err != nil {
		return users.NewChangeOwnPasswordInternalServerError().WithPayload(internalError("Password changed, but existing sessions could not be invalidated", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpPasswordChange,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        currentUser.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     currentUser.Username,
		Details:      map[string]any{"flow": "self_service"},
	})

	return users.NewChangeOwnPasswordNoContent()
}

func ChangeUserPassword(params users.ChangeUserPasswordParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return users.NewChangeUserPasswordInternalServerError().WithPayload(internalError("Failed to initialize user service", err))
	}
	actor, errPayload := requireSystemAdmin(principal)
	if errPayload != nil {
		if errPayload.Code == http.StatusUnauthorized {
			return users.NewChangeUserPasswordUnauthorized().WithPayload(errPayload)
		}
		return users.NewChangeUserPasswordForbidden().WithPayload(errPayload)
	}
	if params.Body == nil || params.Body.NewPassword == nil {
		return users.NewChangeUserPasswordBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	newPassword := string(*params.Body.NewPassword)
	if err := svc.passwordPolicy.Validate(newPassword); err != nil {
		return users.NewChangeUserPasswordBadRequest().WithPayload(appError(err.Error(), http.StatusBadRequest))
	}

	user, err := svc.queries.GetUserByUsername(params.HTTPRequest.Context(), params.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return users.NewChangeUserPasswordNotFound().WithPayload(appError("User not found", http.StatusNotFound))
		}
		return users.NewChangeUserPasswordInternalServerError().WithPayload(internalError("Failed to load user for password reset", err))
	}

	hash, err := gcauth.HashPassword(newPassword)
	if err != nil {
		return users.NewChangeUserPasswordInternalServerError().WithPayload(internalError("Failed to hash new password", err))
	}
	if err := svc.queries.UpdateUserPassword(params.HTTPRequest.Context(), database.UpdateUserPasswordParams{
		Username:     params.Username,
		PasswordHash: hash,
	}); err != nil {
		return users.NewChangeUserPasswordInternalServerError().WithPayload(internalError("Failed to reset user password", err))
	}
	if err := svc.queries.DeleteUserSessions(params.HTTPRequest.Context(), user.ID); err != nil {
		return users.NewChangeUserPasswordInternalServerError().WithPayload(internalError("Password reset, but existing sessions could not be invalidated", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpPasswordChange,
		ResourceType: auditlog.ResUser,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     params.Username,
		Details:      map[string]any{"flow": "admin_reset"},
	})

	return users.NewChangeUserPasswordNoContent()
}

func userResponse(id int32, username, role, createdAt string) *swaggermodels.UserResponse {
	return &swaggermodels.UserResponse{
		ID:        id,
		Username:  username,
		Role:      role,
		CreatedAt: createdAt,
	}
}
