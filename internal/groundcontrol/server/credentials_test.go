package server

import (
	"context"
	"net/http"
	"net/netip"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const wrongPassword = "Wrong-passw0rd"

var (
	clientIP      = netip.MustParseAddr("192.0.2.10")
	otherClientIP = netip.MustParseAddr("192.0.2.20")
)

func TestVerifyUserCredentials_WrongPasswordIsInvalid(t *testing.T) {
	pending.Plan(t, "D6", "Login and Basic auth check user credentials separately")
	h := newGCHarness(t)

	_, err := h.app.verifyUserCredentials(context.Background(), clientIP, "admin", wrongPassword)

	require.ErrorIs(t, err, errInvalidCredentials)
}

func TestVerifyUserCredentials_UnknownUserIsInvalid(t *testing.T) {
	pending.Plan(t, "D6", "Login and Basic auth check user credentials separately")
	h := newGCHarness(t)

	_, err := h.app.verifyUserCredentials(context.Background(), clientIP, "nobody", wrongPassword)

	require.ErrorIs(t, err, errInvalidCredentials)
}

func TestVerifyUserCredentials_LocksAfterMaxFailures(t *testing.T) {
	pending.Plan(t, "D6", "Login and Basic auth check user credentials separately")
	h := newGCHarness(t)
	ctx := context.Background()
	for range maxFailedAttempts {
		_, err := h.app.verifyUserCredentials(ctx, clientIP, "admin", wrongPassword)
		require.ErrorIs(t, err, errInvalidCredentials)
	}

	_, err := h.app.verifyUserCredentials(ctx, clientIP, "admin", harnessAdminPassword)

	require.ErrorIs(t, err, errAccountLocked)
}

func TestVerifyUserCredentials_SuccessReturnsUser(t *testing.T) {
	pending.Plan(t, "D6", "Login and Basic auth check user credentials separately")
	h := newGCHarness(t)

	user, err := h.app.verifyUserCredentials(context.Background(), clientIP, "admin", harnessAdminPassword)

	require.NoError(t, err)
	require.Equal(t, "admin", user.Username)
}

func TestBasicAuth_FailuresCountTowardLoginLockout(t *testing.T) {
	pending.Plan(t, "D6", "failed Basic auth attempts are not recorded, so they never lock the account")
	h := newGCHarness(t)
	for range 3 {
		h.mustDo(http.MethodGet, "/api/configs", nil, basicAuth("admin", wrongPassword), http.StatusUnauthorized)
	}
	for range 2 {
		h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: wrongPassword}, nil, http.StatusUnauthorized)
	}

	h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil, http.StatusUnauthorized)
}

func TestBasicAuth_SuccessResetsAttempts(t *testing.T) {
	pending.Plan(t, "D6", "a successful Basic auth does not reset the failed attempts /login counts")
	h := newGCHarness(t)
	for range 3 {
		h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: wrongPassword}, nil, http.StatusUnauthorized)
	}
	h.mustDo(http.MethodGet, "/api/configs", nil, adminBasicAuth, http.StatusOK)
	for range 3 {
		h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: wrongPassword}, nil, http.StatusUnauthorized)
	}

	h.mustDo(http.MethodPost, "/login", LoginRequest{Username: "admin", Password: harnessAdminPassword}, nil, http.StatusOK)
}

func TestVerifyUserCredentials_LimitsFailuresPerClientIP(t *testing.T) {
	pending.Plan(t, "D6", "failed authentications are not limited per client IP")
	t.Setenv("AUTH_FAILURE_LIMIT", "3")
	h := newGCHarness(t)
	ctx := context.Background()
	for _, user := range []string{"alice", "bob", "carol"} {
		_, err := h.app.verifyUserCredentials(ctx, clientIP, user, wrongPassword)
		require.ErrorIs(t, err, errInvalidCredentials)
	}

	_, err := h.app.verifyUserCredentials(ctx, clientIP, "admin", harnessAdminPassword)
	require.ErrorIs(t, err, errTooManyAttempts, "the blocked IP must not get to check a password")

	user, err := h.app.verifyUserCredentials(ctx, otherClientIP, "admin", harnessAdminPassword)
	require.NoError(t, err)
	require.Equal(t, "admin", user.Username)
}

func TestBasicAuth_RateLimitsFailuresPerClientIP(t *testing.T) {
	pending.Plan(t, "D6", "failed Basic auth attempts are not rate limited")
	t.Setenv("AUTH_FAILURE_LIMIT", "3")
	h := newGCHarness(t)
	for _, user := range []string{"alice", "bob", "carol"} {
		h.mustDo(http.MethodGet, "/api/configs", nil, basicAuth(user, wrongPassword), http.StatusUnauthorized)
	}

	h.mustDo(http.MethodGet, "/api/configs", nil, adminBasicAuth, http.StatusTooManyRequests)
}
