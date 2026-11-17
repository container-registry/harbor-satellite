package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/auth"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	"github.com/container-registry/harbor-satellite/internal/shared/crypto"
	auditlog "github.com/container-registry/harbor-satellite/internal/shared/logger"
)

var (
	errTooManyAttempts    = errors.New("too many failed attempts")
	errAccountLocked      = errors.New("account locked")
	errInvalidCredentials = errors.New("invalid credentials")
	errUnknownUser        = fmt.Errorf("%w: unknown user", errInvalidCredentials)
	errBadPassword        = fmt.Errorf("%w: wrong password", errInvalidCredentials)
	// errPasswordLoginDisabled: with OIDC_DISABLE_PASSWORD_LOGIN every user
	// except the bootstrap admin, the break-glass account, fails with it
	// (plan item 8.2).
	errPasswordLoginDisabled = errors.New("password login disabled")
)

// verifyUserCredentials is the one user credential check behind Login and
// Basic auth, and it owns every failure counter: an IP the FailureLimiter
// blocks fails with errTooManyAttempts before the password is checked, a
// locked account with errAccountLocked, and a wrong password or unknown user
// records a failed attempt for the username and the IP and fails with
// errInvalidCredentials. A success resets the username's attempts.
func (s *Server) verifyUserCredentials(ctx context.Context, clientIP netip.Addr, username, password string) (database.User, error) {
	if s.FailureLimiter.Blocked(clientIP) {
		return database.User{}, errTooManyAttempts
	}
	if s.disablePasswordLogin && username != systemAdminUsername {
		s.FailureLimiter.RecordFailure(clientIP)
		return database.User{}, errPasswordLoginDisabled
	}

	attempts, err := s.dbQueries.GetLoginAttempts(ctx, username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return database.User{}, fmt.Errorf("read login attempts: %w", err)
	}
	if err == nil && attempts.LockedUntil.Valid && attempts.LockedUntil.Time.After(time.Now()) {
		s.FailureLimiter.RecordFailure(clientIP)
		return database.User{}, errAccountLocked
	}

	user, err := s.dbQueries.GetUserByUsername(ctx, username)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Same Argon2id cost as a real check, so timing does not reveal
		// whether the user exists.
		_ = crypto.VerifySecret(password, crypto.TimingMitigationHash)
		s.recordFailure(ctx, clientIP, username)
		return database.User{}, errUnknownUser
	case err != nil:
		return database.User{}, fmt.Errorf("read user: %w", err)
	}

	if !auth.VerifyPassword(password, user.PasswordHash) {
		s.recordFailure(ctx, clientIP, username)
		return database.User{}, errBadPassword
	}

	if err := s.dbQueries.ResetLoginAttempts(ctx, username); err != nil {
		return database.User{}, fmt.Errorf("reset login attempts: %w", err)
	}
	return user, nil
}

// recordFailure counts a failed check for the IP and the username, and
// locks the username once it reaches maxFailedAttempts.
func (s *Server) recordFailure(ctx context.Context, clientIP netip.Addr, username string) {
	s.FailureLimiter.RecordFailure(clientIP)
	attempts, err := s.dbQueries.UpsertLoginAttempt(ctx, username)
	if err != nil {
		return
	}
	if attempts.FailedCount >= maxFailedAttempts {
		// locked_until is TIMESTAMP without time zone and is read back as
		// UTC. A local wall time would make a lock on a host west of UTC
		// expire at once, and one east of it last hours too long.
		lockUntil := time.Now().UTC().Add(s.lockoutDuration)
		if err := s.dbQueries.LockAccount(ctx, database.LockAccountParams{
			Username:    username,
			LockedUntil: sql.NullTime{Time: lockUntil, Valid: true},
		}); err != nil {
			log.Printf("Failed to lock account %s: %v", username, err)
		}
	}
}

// credentialFailureReason maps a verifyUserCredentials error to its audit reason.
func credentialFailureReason(err error) auditlog.Reason {
	switch {
	case errors.Is(err, errTooManyAttempts):
		return auditlog.ReasonRateLimited
	case errors.Is(err, errAccountLocked):
		return auditlog.ReasonAccountLocked
	case errors.Is(err, errUnknownUser):
		return auditlog.ReasonUnknownUser
	case errors.Is(err, errBadPassword):
		return auditlog.ReasonBadPassword
	case errors.Is(err, errPasswordLoginDisabled):
		return auditlog.ReasonForbidden
	default:
		return auditlog.ReasonInvalidCredentials
	}
}

// writeCredentialError answers a failed credential check. A locked account
// looks like wrong credentials so the response does not confirm the user.
func writeCredentialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTooManyAttempts):
		WriteJSONError(w, "Too Many Requests", http.StatusTooManyRequests)
	case errors.Is(err, errAccountLocked), errors.Is(err, errInvalidCredentials):
		WriteJSONError(w, "Invalid credentials", http.StatusUnauthorized)
	case errors.Is(err, errPasswordLoginDisabled):
		WriteJSONError(w, "Invalid credentials: "+errPasswordLoginDisabled.Error()+"; log in through OIDC", http.StatusUnauthorized)
	default:
		WriteJSONError(w, "Internal server error", http.StatusInternalServerError)
	}
}

// clientAddr is the request's client IP for per-IP limits: clientIP, which
// honours trustForwardedHeaders, then the peer. A forwarded entry that does
// not parse falls back to the peer, the proxy, whose bucket those clients
// then share.
func (s *Server) clientAddr(r *http.Request) netip.Addr {
	if addr, err := netip.ParseAddr(s.clientIP(r)); err == nil {
		return addr.Unmap()
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.Unmap()
		}
	}
	return netip.Addr{}
}
