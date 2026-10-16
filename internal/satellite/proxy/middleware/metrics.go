package middleware

import (
	"github.com/container-registry/harbor-satellite/internal/satellite/proxy"
	"github.com/container-registry/harbor-satellite/internal/satellite/telemetry"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// Metrics records every request's resource, outcome and duration with
// rec.ObservePull, rejections included. Stub: every request fails.
func Metrics(rec telemetry.Recorder) proxy.MiddlewareFunc {
	return func(proxy.HandlerFunc) proxy.HandlerFunc {
		return func(*proxy.Request) error {
			return notimpl.Err("9.3 proxy metrics")
		}
	}
}
