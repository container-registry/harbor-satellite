package server

import (
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// StatusForError maps a fleet service error to its HTTP status:
// fleet.ErrNotFound 404; ErrAlreadyExists, ErrConflict and ErrInUse 409;
// ErrInvalid and ErrTokenInvalid 400; ErrTokenExpired, ErrSessionInvalid
// and ErrSessionExpired 401; ErrRevoked and ErrForbidden 403;
// ErrRegistryUnavailable 502; ErrNotProvisioned 503; anything else 500.
func StatusForError(err error) int {
	return http.StatusNotImplemented
}

// AppErrorFor is the response to a fleet service error: StatusForError's
// status, and the message today's handlers send where clients see it. A
// registration token error keeps "Error: Invalid Token" (400) and "Error:
// Token Expired" (401), so the golden responses do not change.
func AppErrorFor(err error) *AppError {
	return &AppError{Code: http.StatusNotImplemented, Message: notimpl.Err("1.4 fleet error response").Error()}
}
