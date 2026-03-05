package middleware

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter tracks request rates per IP address.
type RateLimiter struct {
	mu             sync.RWMutex
	requests       map[string][]time.Time
	maxRequests    int
	windowPeriod   time.Duration
	trustedProxies []*net.IPNet
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(maxRequests int, windowPeriod time.Duration, trustedProxies []string) *RateLimiter {
	rl := &RateLimiter{
		requests:       make(map[string][]time.Time),
		maxRequests:    maxRequests,
		windowPeriod:   windowPeriod,
		trustedProxies: parseProxies(trustedProxies),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// parseProxies converts a slice of IP strings or CIDRs into net.IPNet objects.
func parseProxies(proxies []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, p := range proxies {

		_, ipnet, err := net.ParseCIDR(p)
		if err == nil {
			nets = append(nets, ipnet)
			continue
		}
		ip := net.ParseIP(p)
		if ip != nil {
			if ip.To4() != nil {
				nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)})
			} else {
				nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
			}
		} else {
			log.Printf("Warning: Invalid trusted proxy configuration: %s", p)
		}
	}
	return nets
}

// isTrusted checks if an IP belongs to our trusted proxies list.
func (rl *RateLimiter) isTrusted(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, network := range rl.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
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
			ip := rl.getClientIP(r)

			if !rl.Allow(ip) {
				log.Printf("Rate limit exceeded for IP: %s", ip)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Too Many Requests"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP securely extracts the client IP, evaluating X-Forwarded-For
// from right to left, stopping at the first untrusted IP.
func (rl *RateLimiter) getClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !rl.isTrusted(ip) {
		return ip
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return ip
	}
	ips := strings.Split(xff, ",")

	for i := len(ips) - 1; i >= 0; i-- {
		headerIP := strings.TrimSpace(ips[i])
		if headerIP == "" {
			continue
		}
		parsed := net.ParseIP(headerIP)
		if parsed == nil {
			log.Printf("Warning: invalid X-Forwarded-For hop: %q", headerIP)
			return ip
		}
		candidate := parsed.String()
		ip = candidate
		if !rl.isTrusted(candidate) {
			break
		}
	}
	return ip
}
