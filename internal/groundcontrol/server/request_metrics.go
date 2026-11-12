package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// RequestObserver receives every answered request for /metrics; the
// composition root passes Ground Control's telemetry recorder.
type RequestObserver interface {
	ObserveHTTP(route, method string, code int, d time.Duration)
}

// WithRequestObserver makes the handler report every request to o.
func WithRequestObserver(o RequestObserver) Option {
	return func(s *Server) { s.requests = o }
}

// unmatchedRoute labels requests no route matched, so a scan of random
// paths cannot grow the label set.
const unmatchedRoute = "unmatched"

type routeTemplateKey struct{}

// observeRequests wraps the whole router so 404, 405 and the 401 and 429
// of the security middleware are counted too; recordRouteTemplate, a
// router middleware, fills in the matched template.
func (s *Server) observeRequests(next http.Handler) http.Handler {
	if s.requests == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		route := unmatchedRoute
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), routeTemplateKey{}, &route)))
		s.requests.ObserveHTTP(route, r.Method, rec.code, time.Since(start))
	})
}

func recordRouteTemplate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route, ok := r.Context().Value(routeTemplateKey{}).(*string); ok {
			if current := mux.CurrentRoute(r); current != nil {
				if tpl, err := current.GetPathTemplate(); err == nil {
					*route = tpl
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.code, r.wroteHeader = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
