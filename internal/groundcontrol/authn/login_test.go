package authn_test

import (
	"context"
	"crypto/sha256"
	"net/url"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/authn/authntest"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

type oidcWorld struct {
	provider   *authntest.Provider
	identities *authntest.Identities
	clock      *clock
	login      *authn.OIDCLogin
}

func newOIDCWorld(t *testing.T) *oidcWorld {
	t.Helper()
	w := &oidcWorld{provider: &authntest.Provider{}, identities: authntest.NewIdentities(), clock: &clock{t: start}}
	cfg := oidcCfg
	cfg.LoginStateTTL = 10 * time.Minute
	login, err := authn.NewOIDCLogin(w.provider, w.identities, w.clock, cfg)
	require.NoError(t, err)
	w.login = login
	return w
}

// started runs Start and returns the state the provider was sent and the
// browser binding.
func (w *oidcWorld) started(t *testing.T) (state, binding string) {
	t.Helper()
	start, err := w.login.Start(context.Background())
	require.NoError(t, err)
	u, err := url.Parse(start.RedirectURL)
	require.NoError(t, err)
	return u.Query().Get("state"), start.Binding
}

func TestOIDCLogin_StartUsesPKCENonceAndBinding(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	w := newOIDCWorld(t)

	state, binding := w.started(t)

	require.NotEmpty(t, state)
	require.NotEmpty(t, binding)
	stored := w.identities.States()
	require.Len(t, stored, 1)
	require.Equal(t, state, stored[0].State)
	require.Equal(t, sha256.Sum256([]byte(binding)), stored[0].BindingHash, "only the binding's hash is stored")
	require.Equal(t, start.Add(10*time.Minute), stored[0].ExpiresAt)
	require.NotEmpty(t, stored[0].CodeVerifier)
	require.NotEmpty(t, stored[0].Nonce)
	call := w.provider.Calls()[0]
	require.Equal(t, "AuthCodeURL", call.Method)
	challenge, ok := call.Args[1].(string)
	require.True(t, ok)
	require.NotEqual(t, stored[0].CodeVerifier, challenge, "S256 challenge, never the verifier")
	require.Equal(t, stored[0].Nonce, call.Args[2])
}

func TestOIDCLogin_CallbackNeedsTheBrowserBinding(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	w := newOIDCWorld(t)
	w.provider.Answer = claims("alice", "edge-ops")
	state, _ := w.started(t)

	_, err := w.login.Callback(context.Background(), "code", state, "another-browser")

	require.Error(t, err)
	for _, c := range w.provider.Calls() {
		require.NotEqual(t, "Exchange", c.Method, "the code is never exchanged for another browser")
	}
	require.Zero(t, w.identities.Count("Upsert"))
}

func TestOIDCLogin_CallbackUpsertsMappedUser(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	w := newOIDCWorld(t)
	w.provider.Answer = claims("alice", "edge-ops")
	state, binding := w.started(t)
	stored := w.identities.States()[0]

	principal, err := w.login.Callback(context.Background(), "code", state, binding)

	require.NoError(t, err)
	require.Equal(t, "user:alice", principal.Subject)
	var exchange authntest.Call
	for _, c := range w.provider.Calls() {
		if c.Method == "Exchange" {
			exchange = c
		}
	}
	require.Equal(t, []any{"code", stored.CodeVerifier, stored.Nonce}, exchange.Args)
	require.Equal(t, 1, w.identities.Count("Upsert"))
	_, err = w.login.Callback(context.Background(), "code", state, binding)
	require.Error(t, err, "a state is used once")
}

func TestOIDCLogin_CallbackNeverAdoptsLocalAccount(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	w := newOIDCWorld(t)
	w.identities.LocalUsers["bob"] = true
	w.provider.Answer = claims("bob", "platform")
	state, binding := w.started(t)

	_, err := w.login.Callback(context.Background(), "code", state, binding)

	require.ErrorIs(t, err, fleet.ErrForbidden)
}

func TestOIDCLogin_CallbackUnmappedCreatesNoRow(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	w := newOIDCWorld(t)
	w.provider.Answer = claims("carol", "unmapped")
	state, binding := w.started(t)

	_, err := w.login.Callback(context.Background(), "code", state, binding)

	require.ErrorIs(t, err, fleet.ErrForbidden)
	require.Zero(t, w.identities.Count("Upsert"))
}

func TestOIDCLogin_DevicePendingThenPrincipal(t *testing.T) {
	pending.Plan(t, "8.1", pendingOIDC)
	w := newOIDCWorld(t)
	w.provider.Device = fleet.DeviceAuthorization{DeviceCode: "dev", UserCode: "ABCD-EFGH", VerificationURI: "https://idp.example/device", ExpiresIn: 10 * time.Minute, Interval: 5 * time.Second}
	w.provider.PollAnswers = []error{fleet.ErrAuthorizationPending, fleet.ErrSlowDown}
	w.provider.Answer = claims("alice", "edge-ops")

	auth, err := w.login.DeviceStart(context.Background())
	require.NoError(t, err)
	require.Equal(t, w.provider.Device, auth)

	_, err = w.login.DevicePoll(context.Background(), "dev")
	require.ErrorIs(t, err, fleet.ErrAuthorizationPending)
	_, err = w.login.DevicePoll(context.Background(), "dev")
	require.ErrorIs(t, err, fleet.ErrSlowDown)
	principal, err := w.login.DevicePoll(context.Background(), "dev")
	require.NoError(t, err)
	require.Equal(t, "user:alice", principal.Subject)
	require.Equal(t, 1, w.identities.Count("Upsert"))
}
