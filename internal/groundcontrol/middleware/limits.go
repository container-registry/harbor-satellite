package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// Rate is Requests per Window.
type Rate struct {
	Requests int
	Window   time.Duration
}

// ParseRate parses "<requests>/<duration>", e.g. "10/1m" or "3/1s", as
// RATE_LIMIT_ANONYMOUS and RATE_LIMIT_FLEET carry it.
func ParseRate(s string) (Rate, error) {
	n, d, ok := strings.Cut(s, "/")
	requests, err := strconv.Atoi(n)
	if !ok || err != nil || requests <= 0 {
		return Rate{}, fmt.Errorf("rate %q: want <requests>/<duration> with requests > 0", s)
	}
	window, err := time.ParseDuration(d)
	if err != nil || window <= 0 {
		return Rate{}, fmt.Errorf("rate %q: want a positive duration after /", s)
	}
	return Rate{Requests: requests, Window: window}, nil
}

// Limiter allows rate requests per key (plan item 4.6): the anonymous
// endpoints key it by client IP, the fleet endpoints by the authenticated
// satellite, so satellites behind one NAT do not share a budget.
type Limiter[K comparable] struct{}

func NewLimiter[K comparable](rate Rate) *Limiter[K] { return &Limiter[K]{} }

// Allow counts one request of key and reports whether it is within the
// rate. Skeleton: always true.
func (l *Limiter[K]) Allow(key K) bool { return true }

// Stop releases the limiter's background cleanup.
func (l *Limiter[K]) Stop() {}

// LimitBy answers 429 with an AppError body {"code":429,"message":"Too Many Requests"}
// when l does not allow the request's key; a request without a key is
// passed on. Stub: answers 501.
func LimitBy[K comparable](l *Limiter[K], key func(*http.Request) (K, bool)) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, notimpl.Err("4.6 limit by key").Error(), http.StatusNotImplemented)
		})
	}
}
