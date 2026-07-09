package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	gcauth "github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/auth"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
)

func Login(params auth.LoginParams) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return auth.NewLoginInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if params.Body == nil || params.Body.Username == nil || params.Body.Password == nil {
		return auth.NewLoginBadRequest().WithPayload(appError("Invalid request body", http.StatusBadRequest))
	}

	username := *params.Body.Username
	password := string(*params.Body.Password)
	if username == "" || password == "" {
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	attempts, err := svc.queries.GetLoginAttempts(params.HTTPRequest.Context(), username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return auth.NewLoginInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	user, err := svc.queries.GetUserByUsername(params.HTTPRequest.Context(), username)
	if err != nil {
		svc.recordFailedAttempt(params.HTTPRequest.Context(), username)
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	if !gcauth.VerifyPassword(password, user.PasswordHash) {
		svc.recordFailedAttempt(params.HTTPRequest.Context(), username)
		return auth.NewLoginUnauthorized().WithPayload(appError("Invalid credentials", http.StatusUnauthorized))
	}

	_ = svc.queries.ResetLoginAttempts(params.HTTPRequest.Context(), username)

	token, err := gcauth.GenerateSessionToken()
	if err != nil {
		return auth.NewLoginInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	expiresAt := time.Now().Add(svc.sessionDuration)
	if _, err := svc.queries.CreateSession(params.HTTPRequest.Context(), database.CreateSessionParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expiresAt,
	}); err != nil {
		return auth.NewLoginInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	return auth.NewLoginOK().WithPayload(&swaggermodels.LoginResponse{
		Token:     token,
		ExpiresAt: strfmt.DateTime(expiresAt),
	})
}

func Logout(params auth.LogoutParams, principal any) middleware.Responder {
	svc, err := getService()
	if err != nil {
		return auth.NewLogoutInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}
	if _, errPayload := requirePrincipal(principal); errPayload != nil {
		return auth.NewLogoutUnauthorized().WithPayload(errPayload)
	}

	token := ""
	if params.HTTPRequest != nil {
		token = bearerToken(params.HTTPRequest.Header.Get("Authorization"))
	}
	if token == "" {
		return auth.NewLogoutUnauthorized().WithPayload(appError("Unauthorized", http.StatusUnauthorized))
	}

	if err := svc.queries.DeleteSession(params.HTTPRequest.Context(), token); err != nil {
		return auth.NewLogoutInternalServerError().WithPayload(appError("Internal server error", http.StatusInternalServerError))
	}

	return auth.NewLogoutNoContent()
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}

func (s *service) recordFailedAttempt(ctx context.Context, username string) {
	attempts, err := s.queries.UpsertLoginAttempt(ctx, username)
	if err != nil {
		return
	}

	if attempts.FailedCount >= maxFailedAttempts {
		_ = s.queries.LockAccount(ctx, database.LockAccountParams{
			Username:    username,
			LockedUntil: sql.NullTime{Time: time.Now().Add(s.lockoutDuration), Valid: true},
		})
	}
}
