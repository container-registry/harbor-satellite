package server

import (
	"errors"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// StatusForError maps a fleet service error to its HTTP status:
// fleet.ErrNotFound 404; ErrAlreadyExists, ErrConflict and ErrInUse 409;
// ErrInvalid and ErrTokenInvalid 400; ErrTokenExpired, ErrSessionInvalid
// and ErrSessionExpired 401; ErrRevoked and ErrForbidden 403;
// ErrRegistryUnavailable 502; ErrNotProvisioned 503; anything else 500.
func StatusForError(err error) int {
	switch {
	case errors.Is(err, fleet.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, fleet.ErrAlreadyExists), errors.Is(err, fleet.ErrConflict), errors.Is(err, fleet.ErrInUse):
		return http.StatusConflict
	case errors.Is(err, fleet.ErrInvalid), errors.Is(err, fleet.ErrTokenInvalid):
		return http.StatusBadRequest
	case errors.Is(err, fleet.ErrTokenExpired), errors.Is(err, fleet.ErrSessionInvalid), errors.Is(err, fleet.ErrSessionExpired):
		return http.StatusUnauthorized
	case errors.Is(err, fleet.ErrRevoked), errors.Is(err, fleet.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, fleet.ErrRegistryUnavailable):
		return http.StatusBadGateway
	case errors.Is(err, fleet.ErrNotProvisioned):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// AppErrorFor is the response to a fleet service error: StatusForError's
// status, and the message today's handlers send where clients see it. A
// registration token error keeps "Error: Invalid Token" (400) and "Error:
// Token Expired" (401), so the golden responses do not change.
func AppErrorFor(err error) *AppError {
	code := StatusForError(err)
	switch {
	case errors.Is(err, fleet.ErrTokenInvalid):
		return &AppError{Code: code, Message: "Error: Invalid Token"}
	case errors.Is(err, fleet.ErrTokenExpired):
		return &AppError{Code: code, Message: "Error: Token Expired"}
	case code == http.StatusInternalServerError:
		return &AppError{Code: code, Message: "Internal Server Error"}
	default:
		return &AppError{Code: code, Message: err.Error()}
	}
}

// retryAfterProvisioning is the Retry-After of a 503 while the async plane
// has not created a satellite's robot yet.
const retryAfterProvisioning = "5"

// handleFleetError answers err, with Retry-After for ErrNotProvisioned.
func handleFleetError(w http.ResponseWriter, err error, override *AppError) {
	if errors.Is(err, fleet.ErrNotProvisioned) {
		w.Header().Set("Retry-After", retryAfterProvisioning)
	}
	if override != nil {
		HandleAppError(w, override)
		return
	}
	HandleAppError(w, AppErrorFor(err))
}
