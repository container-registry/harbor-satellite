package authn

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type LoginStart struct {
	RedirectURL string
	// Binding is set as an HttpOnly, Secure, SameSite=Lax cookie; Callback
	// needs it back.
	Binding string
}

// OIDCLogin runs the authorization code flow with PKCE, a nonce and a
// browser binding, and the device flow (plan item 8.1).
type OIDCLogin struct{}

func NewOIDCLogin(provider OIDCProvider, identities IdentityStore, clock fleet.Clock, cfg OIDCConfig) (*OIDCLogin, error) {
	return nil, notimpl.Err("8.1 OIDC login")
}

// Start stores a login state (state, code verifier, nonce and the SHA-256
// of a fresh binding) and returns the provider URL and the binding.
func (l *OIDCLogin) Start(ctx context.Context) (LoginStart, error) {
	return LoginStart{}, notimpl.Err("8.1 OIDC login start")
}

// Callback takes the login state for state and the binding's hash, so it
// works once and only in the browser that started it; it exchanges the
// code, maps the claims and upserts the user.
func (l *OIDCLogin) Callback(ctx context.Context, code, state, binding string) (fleet.Principal, error) {
	return fleet.Principal{}, notimpl.Err("8.1 OIDC callback")
}

// DeviceStart raises the provider's Interval to MinDevicePollInterval, so a
// client polling as told never gets 429 from the anonymous limiter.
func (l *OIDCLogin) DeviceStart(ctx context.Context) (fleet.DeviceAuthorization, error) {
	return fleet.DeviceAuthorization{}, notimpl.Err("8.1 OIDC device start")
}

// DevicePoll answers fleet.ErrAuthorizationPending and fleet.ErrSlowDown
// as the provider does.
func (l *OIDCLogin) DevicePoll(ctx context.Context, deviceCode string) (fleet.Principal, error) {
	return fleet.Principal{}, notimpl.Err("8.1 OIDC device poll")
}
