package server

import (
	"context"
	"net"
	"net/http"
	"net/netip"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/middleware"
)

// SatelliteAuthenticator is the satellite half of authn.Sessions; app
// passes it with WithSatelliteAuthenticator, so the server imports no gc-core package.
type SatelliteAuthenticator interface {
	Authenticate(ctx context.Context, token string) (fleet.SatelliteAuth, error)
	Lookup(ctx context.Context, satelliteName string) (fleet.SatelliteAuth, error)
}

// WithSatelliteAuthenticator switches the satellite endpoints to the
// session path of plan item 4.1 (ADR-0016). Without it they keep today's
// chain.
func WithSatelliteAuthenticator(a SatelliteAuthenticator) Option {
	return func(s *Server) { s.satelliteAuth = a }
}

type satelliteAuthKey struct{}

// authenticateSatellite is the one authentication step of the session path:
// an SVID name in the context goes to Lookup (Method svid); any Bearer token
// to Authenticate; Basic, while SATELLITE_ROBOT_AUTH is on, to
// validateRobotAuth and then Lookup (Method robot). The FailureLimiter is
// checked by client IP before the Argon2id verify, and every failure is
// recorded in it. The auth is stored under satelliteAuthKey, then the fleet
// limiter and the handler run; nothing authenticates again. Stub: 501.
func (s *Server) authenticateSatellite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notImplemented(w, "4.1 satellite session authentication")
	})
}

// syncWithAuth is SyncSatellite on the session path: one
// HeartbeatService.Beat with the SatelliteAuth of the context.
func (s *Server) syncWithAuth(w http.ResponseWriter, _ *http.Request) {
	notImplemented(w, "4.2 heartbeat through the fleet service")
}

// fleetLimit limits the satellite endpoints per authenticated satellite,
// so satellites behind one NAT do not share a budget (A66, A124).
func (s *Server) fleetLimit(next http.Handler) http.Handler {
	if s.FleetLimiter == nil {
		return next
	}
	return middleware.LimitBy(s.FleetLimiter, func(r *http.Request) (fleet.SatelliteID, bool) {
		auth, ok := r.Context().Value(satelliteAuthKey{}).(fleet.SatelliteAuth)
		return auth.Satellite, ok
	})(next)
}

// limitAnonymous limits an unauthenticated endpoint per client IP. Before
// RATE_LIMIT_SPLIT it is the shared legacy limiter; after it the key is
// clientIP, which honours trustForwardedHeaders. A forwarded entry that
// does not parse falls back to the peer, the proxy, whose bucket those
// clients then share.
func (s *Server) limitAnonymous(next http.Handler) http.Handler {
	if s.AnonymousLimiter == nil {
		return middleware.RateLimitMiddleware(s.rateLimiter)(next)
	}
	return middleware.LimitBy(s.AnonymousLimiter, func(r *http.Request) (netip.Addr, bool) {
		if addr, err := netip.ParseAddr(s.clientIP(r)); err == nil {
			return addr.Unmap(), true
		}
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			if addr, err := netip.ParseAddr(host); err == nil {
				return addr.Unmap(), true
			}
		}
		return netip.Addr{}, true
	})(next)
}
