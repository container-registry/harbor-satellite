package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	auditlog "github.com/container-registry/harbor-satellite/internal/groundcontrol/logger"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/auth"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
)

func Login(params auth.LoginParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return auth.NewLoginInternalServerError().WithPayload(internalError("Failed to initialize authentication service", err))
	}
	if params.Body == nil || params.Body.Username == nil || params.Body.Password == nil {
		svc.auditEvent(params.HTTPRequest, loginAuditEvent("", auditlog.OutcomeFailure, auditlog.ReasonMissingCredentials))
		return auth.NewLoginBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	username := *params.Body.Username
	password := string(*params.Body.Password)
	if username == "" || password == "" {
		svc.auditEvent(params.HTTPRequest, loginAuditEvent(username, auditlog.OutcomeFailure, auditlog.ReasonMissingCredentials))
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	attempts, err := svc.queries.GetLoginAttempts(params.HTTPRequest.Context(), username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return auth.NewLoginInternalServerError().WithPayload(internalError("Failed to load login attempt state", err))
	}

	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		svc.auditEvent(params.HTTPRequest, loginAuditEvent(username, auditlog.OutcomeFailure, auditlog.ReasonAccountLocked))
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	user, err := svc.queries.GetUserByUsername(params.HTTPRequest.Context(), username)
	if err != nil {
		svc.recordFailedAttempt(params.HTTPRequest.Context(), username)
		svc.auditEvent(params.HTTPRequest, loginAuditEvent(username, auditlog.OutcomeFailure, auditlog.ReasonUnknownUser))
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	if !gcauth.VerifyPassword(password, user.PasswordHash) {
		svc.recordFailedAttempt(params.HTTPRequest.Context(), username)
		svc.auditEvent(params.HTTPRequest, loginAuditEvent(username, auditlog.OutcomeFailure, auditlog.ReasonBadPassword))
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	_ = svc.queries.ResetLoginAttempts(params.HTTPRequest.Context(), username) //nolint:errcheck // Reset failed attempts on success (ignore errors)

	token, err := gcauth.GenerateSessionToken()
	if err != nil {
		return auth.NewLoginInternalServerError().WithPayload(internalError("Failed to generate session token", err))
	}

	expiresAt := time.Now().Add(svc.sessionDuration)
	if _, err := svc.queries.CreateSession(params.HTTPRequest.Context(), database.CreateSessionParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	}); err != nil {
		return auth.NewLoginInternalServerError().WithPayload(internalError("Failed to store user session", err))
	}
	svc.auditEvent(params.HTTPRequest, loginAuditEvent(username, auditlog.OutcomeSuccess, ""))

	return auth.NewLoginOK().WithPayload(&swaggermodels.LoginResponse{
		Token:     token,
		ExpiresAt: strfmt.DateTime(expiresAt),
	})
}

func Logout(params auth.LogoutParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return auth.NewLogoutInternalServerError().WithPayload(internalError("Failed to initialize authentication service", err))
	}
	actor, errPayload := requirePrincipal(principal)
	if errPayload != nil {
		return auth.NewLogoutUnauthorized().WithPayload(errPayload)
	}

	token := ""
	if params.HTTPRequest != nil {
		token = sessionToken(params.HTTPRequest.Header.Get("Authorization"))
	}
	if token == "" {
		return auth.NewLogoutUnauthorized().WithPayload(appError("Unauthorized", http.StatusUnauthorized))
	}

	if err := svc.queries.DeleteSession(params.HTTPRequest.Context(), token); err != nil {
		return auth.NewLogoutInternalServerError().WithPayload(internalError("Failed to delete user session", err))
	}
	svc.auditEvent(params.HTTPRequest, auditlog.AuditEvent{
		Operation:    auditlog.OpDelete,
		ResourceType: auditlog.ResSession,
		Outcome:      auditlog.OutcomeSuccess,
		Actor:        actor.Username,
		ActorType:    auditlog.ActorUser,
		Resource:     actor.Username,
	})

	return auth.NewLogoutNoContent()
}

func loginAuditEvent(username string, outcome auditlog.Outcome, reason auditlog.Reason) auditlog.AuditEvent {
	return auditlog.AuditEvent{
		Operation:    auditlog.OpLogin,
		ResourceType: auditlog.ResSession,
		Outcome:      outcome,
		Actor:        username,
		ActorType:    auditlog.ActorUser,
		Reason:       reason,
	}
}

func sessionToken(header string) string {
	parts := strings.Fields(header)
	switch {
	case len(parts) == 1 && !strings.EqualFold(parts[0], "Bearer"):
		return parts[0]
	case len(parts) == 2 && strings.EqualFold(parts[0], "Bearer"):
		return parts[1]
	default:
		return ""
	}
}

func (s *service) recordFailedAttempt(ctx context.Context, username string) {
	attempts, err := s.queries.UpsertLoginAttempt(ctx, username)
	if err != nil {
		return
	}

	if attempts.FailedCount >= maxFailedAttempts {
		if err := s.queries.LockAccount(ctx, database.LockAccountParams{
			Username:    username,
			LockedUntil: sql.NullTime{Time: time.Now().Add(s.lockoutDuration), Valid: true},
		}); err != nil {
			log.Printf("failed to lock account %q: %v", username, err)
		}
	}
}
