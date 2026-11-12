package admission

import (
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// Middleware applies source's bundle to every proxy request:
//   - off (or no bundle) serves every request;
//   - audit logs each denial and serves the request;
//   - enforce answers a denial with the OCI error DENIED (403);
//   - in enforce, a bundle that does not load (an unknown model version)
//     denies every request.
//
// It rebuilds the evaluator, and so drops the decision cache, when the
// bundle digest changes.
func Middleware(source BundleSource) proxy.MiddlewareFunc {
	return func(proxy.HandlerFunc) proxy.HandlerFunc {
		return func(*proxy.Request) error {
			return notimpl.Err("7.5 admission middleware")
		}
	}
}
