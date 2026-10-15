package fleet

import "errors"

// Services return these, wrapped; server.StatusForError maps them to HTTP
// statuses. Adapters map driver and registry errors onto them.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrConflict      = errors.New("concurrent modification")
	ErrInUse         = errors.New("in use")
	ErrInvalid       = errors.New("invalid")
	// ErrTokenInvalid covers a registration token that is unknown or
	// already claimed, so a response never reveals that a token exists.
	ErrTokenInvalid        = errors.New("registration token invalid")
	ErrTokenExpired        = errors.New("registration token expired")
	ErrNotProvisioned      = errors.New("registry account not provisioned yet")
	ErrRevoked             = errors.New("satellite revoked")
	ErrRegistryUnavailable = errors.New("registry unavailable")
	ErrForbidden           = errors.New("forbidden")
	// ErrOutsideTransaction is returned by the event sink of
	// Repository.Autocommit.
	ErrOutsideTransaction = errors.New("events need a transaction")
)
