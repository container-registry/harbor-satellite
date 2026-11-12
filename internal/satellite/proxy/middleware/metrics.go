package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
)

// Metrics records every request's resource, outcome and duration with
// rec.ObservePull, rejections included.
func Metrics(rec telemetry.Recorder) proxy.MiddlewareFunc {
	return func(next proxy.HandlerFunc) proxy.HandlerFunc {
		return func(r *proxy.Request) error {
			started := time.Now()
			err := next(r)
			rec.ObservePull(r.Resource().String(), outcome(err), time.Since(started))
			return err
		}
	}
}

func outcome(err error) string {
	var dist *proxy.DistributionError
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.As(err, &dist):
		switch dist.Code {
		case proxy.ErrorCodeBlobUnknown, proxy.ErrorCodeManifestUnknown:
			return "not_found"
		case proxy.ErrorCodeDenied:
			return "denied"
		case proxy.ErrorCodeUnauthorized:
			return "unauthorized"
		default:
			return "error"
		}
	default:
		return "error"
	}
}
