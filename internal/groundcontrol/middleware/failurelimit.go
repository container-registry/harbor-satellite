package middleware

import (
	"net/netip"
	"time"
)

// FailureLimiter counts failed authentication attempts per client IP and
// blocks an IP that reaches maxFailures within window. It is keyed by IP
// alone to catch password spraying across usernames; the per-username
// lockout lives in login_attempts. A success does not reset the count, so
// an attacker with one valid account cannot clear it.
type FailureLimiter struct{}

func NewFailureLimiter(maxFailures int, window time.Duration) *FailureLimiter {
	return &FailureLimiter{}
}

// Blocked reports whether ip has used up its failures. D6 skeleton: never
// blocks.
func (l *FailureLimiter) Blocked(ip netip.Addr) bool { return false }

// RecordFailure counts one failed attempt from ip.
func (l *FailureLimiter) RecordFailure(ip netip.Addr) {}

// Stop releases the limiter's background cleanup.
func (l *FailureLimiter) Stop() {}
