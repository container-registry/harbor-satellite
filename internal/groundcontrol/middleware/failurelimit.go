package middleware

import (
	"net/netip"
	"sync"
	"time"
)

// FailureLimiter counts failed authentication attempts per client IP and
// blocks an IP that reaches maxFailures within window. It is keyed by IP
// alone to catch password spraying across usernames; the per-username
// lockout lives in login_attempts. A success does not reset the count, so
// an attacker with one valid account cannot clear it.
type FailureLimiter struct {
	mu          sync.Mutex
	failures    map[netip.Addr][]time.Time
	maxFailures int
	window      time.Duration
	done        chan struct{}
	stopOnce    sync.Once
}

// NewFailureLimiter returns a limiter that blocks an IP after maxFailures
// failures within window. A maxFailures below 1 disables blocking.
func NewFailureLimiter(maxFailures int, window time.Duration) *FailureLimiter {
	l := &FailureLimiter{
		failures:    make(map[netip.Addr][]time.Time),
		maxFailures: maxFailures,
		window:      window,
		done:        make(chan struct{}),
	}
	if window > 0 {
		go l.cleanup()
	}
	return l
}

// Blocked reports whether ip has used up its failures in the current window.
func (l *FailureLimiter) Blocked(ip netip.Addr) bool {
	if l == nil || l.maxFailures < 1 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recentLocked(ip)) >= l.maxFailures
}

// RecordFailure counts one failed attempt from ip.
func (l *FailureLimiter) RecordFailure(ip netip.Addr) {
	if l == nil || l.maxFailures < 1 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.recentLocked(ip)
	// Past the limit further failures change nothing, so the slice stays
	// bounded under a flood.
	if len(recent) >= l.maxFailures {
		recent = recent[1:]
	}
	l.failures[ip] = append(recent, time.Now())
}

func (l *FailureLimiter) recentLocked(ip netip.Addr) []time.Time {
	cutoff := time.Now().Add(-l.window)
	times := l.failures[ip]
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	if i == len(times) {
		delete(l.failures, ip)
		return nil
	}
	times = times[i:]
	l.failures[ip] = times
	return times
}

func (l *FailureLimiter) cleanup() {
	ticker := time.NewTicker(l.window)
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
		}
		l.mu.Lock()
		for ip := range l.failures {
			l.recentLocked(ip)
		}
		l.mu.Unlock()
	}
}

// Stop releases the limiter's background cleanup.
func (l *FailureLimiter) Stop() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() { close(l.done) })
}
