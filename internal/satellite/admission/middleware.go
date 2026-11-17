package admission

import (
	"sync"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy/middleware"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/policybundle"
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
	m := &admitter{source: source}
	return func(next proxy.HandlerFunc) proxy.HandlerFunc {
		return func(r *proxy.Request) error {
			if r.Repository == "" {
				return next(r)
			}
			bundle, mode, ok := source.Current()
			if !ok || mode == policybundle.ModeOff {
				return next(r)
			}
			log := logger.FromContext(r.Context())
			e, err := m.evaluator(bundle)
			if err != nil {
				log.Error().Err(err).Str("mode", string(mode)).Msg("admission bundle does not load")
				if mode == policybundle.ModeEnforce {
					return proxy.NewError(proxy.ErrorCodeDenied, "admission policy unavailable", nil)
				}
				return next(r)
			}
			req := request(r)
			d, err := e.Decide(r.Context(), req)
			if err != nil {
				log.Error().Err(err).Msg("admission evaluation failed")
				if mode == policybundle.ModeEnforce {
					return proxy.NewError(proxy.ErrorCodeDenied, "admission policy unavailable", nil)
				}
				return next(r)
			}
			if !d.Allowed {
				log.Warn().Str("mode", string(mode)).Str("repository", req.Repository).Str("reference", req.Reference).
					Str("action", req.Action).Str("subject", req.Subject).Strs("rule", d.Rule).Msg("admission deny")
				if mode == policybundle.ModeEnforce {
					return proxy.NewError(proxy.ErrorCodeDenied, "requested access to the resource is denied by the admission policy", nil)
				}
			}
			return next(r)
		}
	}
}

// admitter keeps one evaluator per bundle digest.
type admitter struct {
	source BundleSource
	mu     sync.Mutex
	digest string
	eval   *Evaluator
	err    error
}

func (a *admitter) evaluator(b policybundle.Bundle) (*Evaluator, error) {
	digest := b.Digest()
	a.mu.Lock()
	defer a.mu.Unlock()
	if digest != a.digest || (a.eval == nil && a.err == nil) {
		a.digest = digest
		a.eval, a.err = NewEvaluator(b)
	}
	return a.eval, a.err
}

func request(r *proxy.Request) Request {
	req := Request{Repository: r.Repository, Action: r.Operation.String(), Reference: "*"}
	if r.Resource() == proxy.ResourceManifest {
		req.Reference = r.Reference
		if req.Reference == "" {
			req.Reference = r.Digest.String()
		}
	}
	if peer, ok := middleware.PeerFromContext(r.Context()); ok {
		req.Subject = peer.ID
	}
	return req
}
