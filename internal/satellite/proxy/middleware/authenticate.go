package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
)

type Peer struct{ ID string }

var ErrUnauthenticated = errors.New("proxy client is not authenticated")

// Verifier authenticates a non-loopback client; G7 keeps the proxy on
// loopback, so none exists yet.
type Verifier interface {
	Verify(ctx context.Context, r *http.Request) (Peer, error)
}

type peerKey struct{}

// Authenticate admits loopback clients; any other client needs v, else the
// request is answered with OCI UNAUTHORIZED.
func Authenticate(v Verifier) proxy.MiddlewareFunc {
	return func(next proxy.HandlerFunc) proxy.HandlerFunc {
		return func(r *proxy.Request) error {
			httpReq := r.HTTPRequest()
			if isLoopback(httpReq.RemoteAddr) {
				return next(r)
			}
			if v == nil {
				return proxy.NewError(proxy.ErrorCodeUnauthorized, "", nil)
			}
			peer, err := v.Verify(r.Context(), httpReq)
			if err != nil {
				logger.FromContext(r.Context()).Warn().Err(err).Str("remote", httpReq.RemoteAddr).Msg("proxy client rejected")
				return proxy.NewError(proxy.ErrorCodeUnauthorized, "", nil)
			}
			r.SetContext(context.WithValue(r.Context(), peerKey{}, peer))
			return next(r)
		}
	}
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

func PeerFromContext(ctx context.Context) (Peer, bool) {
	p, ok := ctx.Value(peerKey{}).(Peer)
	return p, ok
}
