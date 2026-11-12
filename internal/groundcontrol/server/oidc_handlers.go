package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

// oidcBindingCookie ties an OIDC login to the browser that started it.
const oidcBindingCookie = "gc_oidc_binding"

// OperatorLogin is the OIDC login of plan item 8.1; app adapts
// authn.OIDCLogin to it, so the server imports no gc-core package.
type OperatorLogin interface {
	Start(ctx context.Context) (redirectURL, binding string, err error)
	Callback(ctx context.Context, code, state, binding string) (fleet.Principal, error)
	DeviceStart(ctx context.Context) (fleet.DeviceAuthorization, error)
	DevicePoll(ctx context.Context, deviceCode string) (fleet.Principal, error)
}

// WithOperatorLogin enables the /oidc routes; without it they answer 404
// once implemented.
func WithOperatorLogin(login OperatorLogin) Option {
	return func(s *Server) { s.operatorLogin = login }
}

func (s *Server) oidcDisabled(w http.ResponseWriter) bool {
	if s.operatorLogin != nil {
		return false
	}
	HandleAppError(w, &AppError{Code: http.StatusNotFound, Message: "OIDC login is not enabled"})
	return true
}

// OidcLogin redirects to the provider and sets the browser binding cookie.
func (s *Server) OidcLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidcDisabled(w) {
		return
	}
	redirect, binding, err := s.operatorLogin.Start(r.Context())
	if err != nil {
		log.Printf("OIDC login start: %v", err)
		HandleAppError(w, &AppError{Code: http.StatusInternalServerError, Message: "OIDC login could not start"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oidcBindingCookie, Value: binding, Path: "/oidc", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int((10 * time.Minute).Seconds()),
	})
	http.Redirect(w, r, redirect, http.StatusFound)
}

// OidcCallback finishes the browser login and answers a Ground Control
// session, like /login.
func (s *Server) OidcCallback(w http.ResponseWriter, r *http.Request, params OidcCallbackParams) {
	if s.oidcDisabled(w) {
		return
	}
	cookie, err := r.Cookie(oidcBindingCookie)
	if err != nil || cookie.Value == "" || params.Code == "" || params.State == "" {
		HandleAppError(w, &AppError{Code: http.StatusBadRequest, Message: "the callback needs code, state and the login's browser cookie"})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcBindingCookie, Path: "/oidc", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	principal, err := s.operatorLogin.Callback(r.Context(), params.Code, params.State, cookie.Value)
	s.issueOperatorSession(w, r, "oidc_callback", principal, err)
}

func (s *Server) OidcDeviceAuthorize(w http.ResponseWriter, r *http.Request) {
	if s.oidcDisabled(w) {
		return
	}
	auth, err := s.operatorLogin.DeviceStart(r.Context())
	if err != nil {
		log.Printf("OIDC device authorization: %v", err)
		HandleAppError(w, &AppError{Code: http.StatusInternalServerError, Message: "device authorization failed"})
		return
	}
	WriteJSONResponse(w, http.StatusOK, DeviceAuthorizationResponse{
		DeviceCode: auth.DeviceCode, UserCode: auth.UserCode, VerificationURI: auth.VerificationURI,
		VerificationURIComplete: auth.VerificationURIComplete,
		ExpiresIn:               int64(auth.ExpiresIn.Seconds()), Interval: int64(auth.Interval.Seconds()),
	})
}

func (s *Server) OidcDeviceToken(w http.ResponseWriter, r *http.Request) {
	if s.oidcDisabled(w) {
		return
	}
	var req DeviceTokenRequest
	if err := DecodeRequestBody(r, &req); err != nil {
		HandleAppError(w, err)
		return
	}
	if req.DeviceCode == "" {
		HandleAppError(w, &AppError{Code: http.StatusBadRequest, Message: "device_code is required"})
		return
	}
	principal, err := s.operatorLogin.DevicePoll(r.Context(), req.DeviceCode)
	switch {
	case errors.Is(err, fleet.ErrAuthorizationPending), errors.Is(err, fleet.ErrSlowDown):
		HandleAppError(w, &AppError{Code: http.StatusBadRequest, Message: err.Error()})
		return
	case err != nil && !errors.Is(err, fleet.ErrForbidden):
		log.Printf("OIDC device token: %v", err)
		HandleAppError(w, &AppError{Code: http.StatusBadRequest, Message: "the device code is unknown or expired"})
		return
	}
	s.issueOperatorSession(w, r, "oidc_device", principal, err)
}

// issueOperatorSession creates the Ground Control session of an OIDC
// principal, whose user the login stored.
func (s *Server) issueOperatorSession(w http.ResponseWriter, r *http.Request, flow string, principal fleet.Principal, err error) {
	username := strings.TrimPrefix(principal.Subject, fleet.SubjectUser)
	if err != nil {
		reason := auditlog.ReasonInvalidCredentials
		if errors.Is(err, fleet.ErrForbidden) {
			reason = auditlog.ReasonForbidden
		}
		s.auditEvent(r, auditlog.AuditEvent{
			Operation: auditlog.OpLogin, ResourceType: auditlog.ResSession, Outcome: auditlog.OutcomeFailure,
			Actor: username, ActorType: auditlog.ActorUser, Reason: reason, Details: map[string]any{"flow": flow},
		})
		if errors.Is(err, fleet.ErrForbidden) {
			HandleAppError(w, &AppError{Code: http.StatusForbidden, Message: "Forbidden: no role is mapped for this user"})
			return
		}
		log.Printf("OIDC %s: %v", flow, err)
		HandleAppError(w, &AppError{Code: http.StatusBadRequest, Message: "OIDC login failed"})
		return
	}
	user, err := s.dbQueries.GetUserByUsername(r.Context(), username)
	if err != nil {
		log.Printf("OIDC %s: user %s: %v", flow, username, err)
		HandleAppError(w, &AppError{Code: http.StatusInternalServerError, Message: "Internal server error"})
		return
	}
	token, err := auth.GenerateSessionToken()
	if err != nil {
		HandleAppError(w, &AppError{Code: http.StatusInternalServerError, Message: "Internal server error"})
		return
	}
	expiresAt := time.Now().Add(s.sessionDuration)
	if _, err := s.dbQueries.CreateSession(r.Context(), database.CreateSessionParams{UserID: user.ID, Token: token, ExpiresAt: expiresAt}); err != nil {
		HandleAppError(w, &AppError{Code: http.StatusInternalServerError, Message: "Internal server error"})
		return
	}
	s.auditEvent(r, auditlog.AuditEvent{
		Operation: auditlog.OpLogin, ResourceType: auditlog.ResSession, Outcome: auditlog.OutcomeSuccess,
		Actor: username, ActorType: auditlog.ActorUser, Details: map[string]any{"flow": flow},
	})
	WriteJSONResponse(w, http.StatusOK, LoginResponse{Token: token, ExpiresAt: expiresAt})
}
