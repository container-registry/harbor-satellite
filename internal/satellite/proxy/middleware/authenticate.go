package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

type Peer struct{ ID string }

var ErrUnauthenticated = errors.New("proxy client is not authenticated")

// Verifier authenticates a non-loopback client; G7 keeps the proxy on
// loopback, so none exists yet.
type Verifier interface {
	Verify(ctx context.Context, r *http.Request) (Peer, error)
}

// Authenticate admits loopback clients; any other client needs v, else the
// request is answered with OCI UNAUTHORIZED. Stub: every request fails.
func Authenticate(v Verifier) proxy.MiddlewareFunc {
	return func(proxy.HandlerFunc) proxy.HandlerFunc {
		return func(*proxy.Request) error {
			return notimpl.Err("9.3 proxy authentication")
		}
	}
}

func PeerFromContext(ctx context.Context) (Peer, bool) { return Peer{}, false }
