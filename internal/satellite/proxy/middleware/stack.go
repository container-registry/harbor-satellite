package middleware

import "github.com/container-registry/harbor-satellite/internal/satellite/proxy"

// Stack is the WrapAll order: admission innermost, metrics outermost, so
// metrics also measure rejections; WrapAll skips nil entries.
func Stack(admission, authenticate, metrics proxy.MiddlewareFunc) []proxy.MiddlewareFunc {
	return []proxy.MiddlewareFunc{admission, authenticate, metrics}
}
