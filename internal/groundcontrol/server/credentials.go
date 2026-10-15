package server

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var (
	errTooManyAttempts    = errors.New("too many failed attempts")
	errAccountLocked      = errors.New("account locked")
	errInvalidCredentials = errors.New("invalid credentials")
)

// verifyUserCredentials is the one user credential check behind Login and
// Basic auth, and it owns every failure counter: an IP the FailureLimiter
// blocks fails with errTooManyAttempts before the password is checked, a
// locked account with errAccountLocked, and a wrong password or unknown user
// records a failed attempt for the username and the IP and fails with
// errInvalidCredentials. A success resets the username's attempts.
func (s *Server) verifyUserCredentials(_ context.Context, clientIP netip.Addr, username, _ string) (database.User, error) { //nolint:unparam // D6 skeleton: the implementation returns the verified user
	return database.User{}, fmt.Errorf("%w: D6 credential check for %q from %s", notimpl.ErrNotImplemented, username, clientIP)
}
