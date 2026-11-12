package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

type LoginStart struct {
	RedirectURL string
	// Binding is set as an HttpOnly, Secure, SameSite=Lax cookie; Callback
	// needs it back.
	Binding string
}

// OIDCLogin runs the authorization code flow with PKCE, a nonce and a
// browser binding, and the device flow (plan item 8.1).
type OIDCLogin struct {
	provider   OIDCProvider
	identities IdentityStore
	clock      fleet.Clock
	cfg        OIDCConfig
}

func NewOIDCLogin(provider OIDCProvider, identities IdentityStore, clock fleet.Clock, cfg OIDCConfig) (*OIDCLogin, error) {
	if provider == nil || identities == nil {
		return nil, fmt.Errorf("%w: the OIDC login needs a provider and an identity store", fleet.ErrInvalid)
	}
	if cfg.LoginStateTTL <= 0 {
		return nil, fmt.Errorf("%w: OIDC_LOGIN_STATE_TTL must be positive", fleet.ErrInvalid)
	}
	if clock == nil {
		clock = fleet.SystemClock{}
	}
	return &OIDCLogin{provider: provider, identities: identities, clock: clock, cfg: cfg}, nil
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// codeChallenge is PKCE's S256 challenge of verifier.
func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Start stores a login state (state, code verifier, nonce and the SHA-256
// of a fresh binding) and returns the provider URL and the binding.
func (l *OIDCLogin) Start(ctx context.Context) (LoginStart, error) {
	var vals [4]string
	for i := range vals {
		v, err := randomString()
		if err != nil {
			return LoginStart{}, err
		}
		vals[i] = v
	}
	state, verifier, nonce, binding := vals[0], vals[1], vals[2], vals[3]
	if err := l.identities.SaveLoginState(ctx, LoginState{
		State: state, CodeVerifier: verifier, Nonce: nonce,
		BindingHash: sha256.Sum256([]byte(binding)), ExpiresAt: l.clock.Now().Add(l.cfg.LoginStateTTL),
	}); err != nil {
		return LoginStart{}, fmt.Errorf("store login state: %w", err)
	}
	redirect, err := l.provider.AuthCodeURL(state, codeChallenge(verifier), nonce)
	if err != nil {
		return LoginStart{}, fmt.Errorf("authorization URL: %w", err)
	}
	return LoginStart{RedirectURL: redirect, Binding: binding}, nil
}

// Callback takes the login state for state and the binding's hash, so it
// works once and only in the browser that started it; it exchanges the
// code, maps the claims and upserts the user.
func (l *OIDCLogin) Callback(ctx context.Context, code, state, binding string) (fleet.Principal, error) {
	if code == "" || state == "" || binding == "" {
		return fleet.Principal{}, fmt.Errorf("%w: the callback needs a code, a state and the browser binding", fleet.ErrInvalid)
	}
	verifier, nonce, err := l.identities.TakeLoginState(ctx, state, sha256.Sum256([]byte(binding)), l.clock.Now())
	if err != nil {
		return fleet.Principal{}, fmt.Errorf("login state: %w", err)
	}
	claims, err := l.provider.Exchange(ctx, code, verifier, nonce)
	if err != nil {
		return fleet.Principal{}, fmt.Errorf("exchange the authorization code: %w", err)
	}
	return l.admit(ctx, claims)
}

// admit maps the claims and stores the user; an unmapped user stores
// nothing.
func (l *OIDCLogin) admit(ctx context.Context, claims Claims) (fleet.Principal, error) {
	principal, role, err := MapPrincipal(claims, l.cfg)
	if err != nil {
		return fleet.Principal{}, err
	}
	if _, err := l.identities.Upsert(ctx, claims, role); err != nil {
		return fleet.Principal{}, fmt.Errorf("store OIDC user %s: %w", claims.Username, err)
	}
	return principal, nil
}

// DeviceStart raises the provider's Interval to MinDevicePollInterval, so a
// client polling as told never gets 429 from the anonymous limiter.
func (l *OIDCLogin) DeviceStart(ctx context.Context) (fleet.DeviceAuthorization, error) {
	auth, err := l.provider.DeviceAuthorize(ctx)
	if err != nil {
		return fleet.DeviceAuthorization{}, fmt.Errorf("device authorization: %w", err)
	}
	auth.Interval = max(auth.Interval, l.cfg.MinDevicePollInterval)
	return auth, nil
}

// DevicePoll answers fleet.ErrAuthorizationPending and fleet.ErrSlowDown
// as the provider does.
func (l *OIDCLogin) DevicePoll(ctx context.Context, deviceCode string) (fleet.Principal, error) {
	if deviceCode == "" {
		return fleet.Principal{}, fmt.Errorf("%w: device_code is required", fleet.ErrInvalid)
	}
	claims, err := l.provider.PollDeviceToken(ctx, deviceCode)
	if err != nil {
		return fleet.Principal{}, err
	}
	return l.admit(ctx, claims)
}
