package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rate is Requests per Window.
type Rate struct {
	Requests int
	Window   time.Duration
}

// LegacyRate is the one per-IP limiter shared by every anonymous and
// satellite endpoint when RATE_LIMIT_SPLIT is off.
var LegacyRate = Rate{Requests: 10, Window: time.Minute}

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
// satellite, so satellites behind one NAT do not share a budget. Each key
// has a token bucket of Requests tokens refilled over Window, so a key
// may burst up to Requests and then gets Requests per Window.
type Limiter[K comparable] struct {
	rate     Rate
	mu       sync.Mutex
	buckets  map[K]*bucket
	now      func() time.Time
	done     chan struct{}
	stopOnce sync.Once
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewLimiter[K comparable](rate Rate) *Limiter[K] {
	l := &Limiter[K]{rate: rate, buckets: map[K]*bucket{}, now: time.Now, done: make(chan struct{})}
	go l.cleanup()
	return l
}

// Allow counts one request of key and reports whether it is within the
// rate.
func (l *Limiter[K]) Allow(key K) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(l.rate.Requests), last: now}
		l.buckets[key] = b
	}
	b.refill(l.rate, now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *bucket) refill(rate Rate, now time.Time) {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.tokens = min(float64(rate.Requests), b.tokens+elapsed.Seconds()*float64(rate.Requests)/rate.Window.Seconds())
	b.last = now
}

// cleanup drops buckets that refilled completely, which hold no state a
// fresh bucket would not have.
func (l *Limiter[K]) cleanup() {
	ticker := time.NewTicker(max(l.rate.Window, time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			l.mu.Lock()
			now := l.now()
			for k, b := range l.buckets {
				b.refill(l.rate, now)
				if b.tokens >= float64(l.rate.Requests) {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// Stop releases the limiter's background cleanup.
func (l *Limiter[K]) Stop() {
	l.stopOnce.Do(func() { close(l.done) })
}

// LimitBy answers 429 with an AppError body {"code":429,"message":"Too Many Requests"}
// when l does not allow the request's key; a request without a key is
// passed on.
func LimitBy[K comparable](l *Limiter[K], key func(*http.Request) (K, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if k, ok := key(r); ok && !l.Allow(k) {
				WriteTooManyRequests(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WriteTooManyRequests writes the server's AppError for 429.
func WriteTooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	if err := json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{http.StatusTooManyRequests, "Too Many Requests"}); err != nil {
		log.Printf("write rate limit response: %v", err)
	}
}
