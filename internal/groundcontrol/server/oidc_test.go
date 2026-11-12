package server

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

const pendingOIDC = "operators log in with local passwords only; the /oidc routes answer 501"

// fakeOperatorLogin answers Callback with principal or err and records the
// binding it was given.
type fakeOperatorLogin struct {
	mu        sync.Mutex
	principal fleet.Principal
	err       error
	bindings  []string
}

func (f *fakeOperatorLogin) Start(context.Context) (string, string, error) {
	return "https://idp.example/authorize", "binding", nil
}

func (f *fakeOperatorLogin) Callback(_ context.Context, _, _, binding string) (fleet.Principal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindings = append(f.bindings, binding)
	return f.principal, f.err
}

func (f *fakeOperatorLogin) DeviceStart(context.Context) (fleet.DeviceAuthorization, error) {
	return fleet.DeviceAuthorization{}, nil
}

func (f *fakeOperatorLogin) DevicePoll(context.Context, string) (fleet.Principal, error) {
	return f.principal, f.err
}

func (h *gcHarness) oidcCallback(binding string) response {
	h.t.Helper()
	req := h.newRequest(http.MethodGet, "/oidc/callback?code=c&state=s", nil, nil)
	req.AddCookie(&http.Cookie{Name: oidcBindingCookie, Value: binding})
	r, err := send(req)
	require.NoError(h.t, err)
	return r
}

func TestOIDCRoutes_AreServedWithoutAuth(t *testing.T) {
	h := newGCHarness(t, WithOperatorLogin(&fakeOperatorLogin{}))
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/oidc/login"},
		{http.MethodGet, "/oidc/callback?code=c&state=s"},
		{http.MethodPost, "/oidc/device"},
		{http.MethodPost, "/oidc/device/token"},
	} {
		got := h.do(route.method, route.path, nil, nil).Status
		require.NotContains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnauthorized, http.StatusForbidden}, got, route.path)
	}
}

// The /oidc routes share the anonymous per-IP bucket, also under the split
// that leaves /login to the FailureLimiter.
func TestOIDCRoutes_LimitedPerClientIPUnderSplit(t *testing.T) {
	pending.Plan(t, "4.6", "the split limiters answer 501")
	splitLimits(t, "2/1m", "3/1s")
	h := newGCHarness(t, WithOperatorLogin(&fakeOperatorLogin{}))

	for range 2 {
		require.NotEqual(t, http.StatusTooManyRequests, h.do(http.MethodPost, "/oidc/device", nil, nil).Status)
	}
	require.Equal(t, http.StatusTooManyRequests, h.do(http.MethodPost, "/oidc/device/token", nil, nil).Status)
}

func TestOIDCCallback_IssuesSessionForMappedGroup(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	login := &fakeOperatorLogin{principal: fleet.Principal{Subject: "user:alice", Roles: []string{"role:admin"}, Groups: []string{"oidc:edge-ops"}}}
	h := newGCHarness(t, WithOperatorLogin(login))

	r := h.oidcCallback("browser-binding")

	require.Equal(t, http.StatusOK, r.Status, string(r.Body))
	var session LoginResponse
	r.decode(t, &session)
	require.NotEmpty(t, session.Token)
	require.Equal(t, []string{"browser-binding"}, login.bindings, "the binding cookie reaches the login")
	h.mustDo(http.MethodGet, "/api/satellites", nil, bearerAuth(session.Token), http.StatusOK)
}

func TestOIDCCallback_UnmappedUserGets403(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	h := newGCHarness(t, WithOperatorLogin(&fakeOperatorLogin{err: fleet.ErrForbidden}))

	r := h.oidcCallback("browser-binding")

	require.Equal(t, http.StatusForbidden, r.Status, string(r.Body))
	var body AppError
	r.decode(t, &body)
	require.Equal(t, http.StatusForbidden, body.Code)
}
