package middleware

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter tracks request rates per IP address.
type RateLimiter struct {
	mu           sync.RWMutex
	requests     map[string][]time.Time
	maxRequests  int
	windowPeriod time.Duration
	stopCh       chan struct{}
	doneCh       chan struct{}
	stopOnce     sync.Once
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(maxRequests int, windowPeriod time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests:     make(map[string][]time.Time),
		maxRequests:  maxRequests,
		windowPeriod: windowPeriod,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// Allow checks if a request from the given IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.windowPeriod)

	// Filter expired timestamps
	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	// Check if rate limit exceeded
	if len(valid) >= rl.maxRequests {
		rl.requests[ip] = valid
		return false
	}

	// Record this request
	rl.requests[ip] = append(valid, now)
	return true
}

// cleanup periodically removes expired entries.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.windowPeriod)
	defer ticker.Stop()
	defer close(rl.doneCh)

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-rl.windowPeriod)

			for ip, timestamps := range rl.requests {
				var valid []time.Time
				for _, t := range timestamps {
					if t.After(cutoff) {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(rl.requests, ip)
				} else {
					rl.requests[ip] = valid
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

// Close stops the cleanup goroutine. Calls are idempotent.
func (rl *RateLimiter) Close() {
	if rl == nil {
		return
	}
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
		<-rl.doneCh
	})
}

// RateLimitMiddleware returns an HTTP middleware that rate limits requests.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

			if !rl.Allow(ip) {
				log.Printf("Rate limit exceeded for IP: %q", ip) //nolint:gosec // RemoteAddr is logged for rate-limit diagnostics purpose.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "Too Many Requests"}); err != nil {
					log.Printf("Failed to write rate limit response: %v", err)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request.
// Only uses RemoteAddr to prevent IP spoofing via X-Forwarded-For/X-Real-IP headers.
// If behind a trusted proxy, configure the proxy to set RemoteAddr correctly.
func getClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
