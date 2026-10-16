package authn

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// OIDCConfig configures operator login through an OIDC provider (plan item
// 8.1, ADR-0018).
type OIDCConfig struct {
	Enabled                                        bool
	IssuerURL, ClientID, ClientSecret, RedirectURL string
	Scopes                                         []string
	UsernameClaim, GroupsClaim                     string
	RoleMap                                        map[string]string // group → "admin" | "system_admin"
	LoginStateTTL                                  time.Duration
	// MinDevicePollInterval is RATE_LIMIT_ANONYMOUS's window divided by its
	// requests, the spacing the /oidc routes' limiter allows.
	MinDevicePollInterval time.Duration
}

type Claims struct {
	Issuer, Subject, Username, Email string
	Groups                           []string
}

type OIDCProvider interface {
	AuthCodeURL(state, codeChallenge, nonce string) (string, error)
	// Exchange verifies the ID token's nonce.
	Exchange(ctx context.Context, code, codeVerifier, nonce string) (Claims, error)
	DeviceAuthorize(ctx context.Context) (fleet.DeviceAuthorization, error)
	PollDeviceToken(ctx context.Context, deviceCode string) (Claims, error)
	VerifyIDToken(ctx context.Context, rawIDToken string) (Claims, error)
}

type LoginState struct {
	State, CodeVerifier, Nonce string
	BindingHash                [sha256.Size]byte
	ExpiresAt                  time.Time
}

type IdentityStore interface {
	// Upsert never adopts a local account: a username held by a user without
	// this (issuer, subject) identity is fleet.ErrForbidden. It sets the
	// user's role to role at every login.
	Upsert(ctx context.Context, claims Claims, role string) (userID int32, err error)
	SaveLoginState(ctx context.Context, state LoginState) error
	TakeLoginState(ctx context.Context, state string, bindingHash [sha256.Size]byte, now time.Time) (codeVerifier, nonce string, err error)
}
