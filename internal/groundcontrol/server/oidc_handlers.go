package server

import (
	"context"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

// oidcBindingCookie ties an OIDC login to the browser that started it.
const oidcBindingCookie = "gc_oidc_binding"

// OperatorLogin is the OIDC login of plan item 8.1; app adapts
// authn.OIDCLogin to it, so the server imports no gc-core package.
type OperatorLogin interface {
	Start(ctx context.Context) (redirectURL, binding string, err error)
	Callback(ctx context.Context, code, state, binding string) (fleet.Principal, error)
	DeviceStart(ctx context.Context) (fleet.DeviceAuthorization, error)
	DevicePoll(ctx context.Context, deviceCode string) (fleet.Principal, error)
}

// WithOperatorLogin enables the /oidc routes; without it they answer 404
// once implemented.
func WithOperatorLogin(login OperatorLogin) Option {
	return func(s *Server) { s.operatorLogin = login }
}

func (s *Server) OidcLogin(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "8.1 OIDC login")
}

func (s *Server) OidcCallback(w http.ResponseWriter, _ *http.Request, _ OidcCallbackParams) {
	notImplemented(w, "8.1 OIDC callback")
}

func (s *Server) OidcDeviceAuthorize(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "8.1 OIDC device authorization")
}

func (s *Server) OidcDeviceToken(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "8.1 OIDC device token")
}
