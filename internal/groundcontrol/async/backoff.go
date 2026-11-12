package async

import "time"

// Backoff is the delay before attempt (1-based) is retried: base doubled
// per earlier attempt, capped at maxDelay.
func Backoff(attempt int, base, maxDelay time.Duration) time.Duration {
	return 0
}
