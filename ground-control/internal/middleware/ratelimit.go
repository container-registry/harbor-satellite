package middleware

import (
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
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(maxRequests int, windowPeriod time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests:     make(map[string][]time.Time),
		maxRequests:  maxRequests,
		windowPeriod: windowPeriod,
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

	for range ticker.C {
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
	}
}

// RateLimitMiddleware returns an HTTP middleware that rate limits requests.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

			if !rl.Allow(ip) {
				log.Printf("Rate limit exceeded for IP: %s", ip)
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP (original client)
		if idx := len(xff); idx > 0 {
			for i := 0; i < len(xff); i++ {
				if xff[i] == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
