package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimiterCloseIsIdempotent(t *testing.T) {
	limiter := NewRateLimiter(1, time.Hour)
	require.NotPanics(t, func() {
		limiter.Close()
		limiter.Close()
	})
}
