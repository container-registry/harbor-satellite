package middleware

import (
	"net/netip"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

var (
	sprayer = netip.MustParseAddr("192.0.2.1")
	other   = netip.MustParseAddr("2001:db8::1")
)

func TestFailureLimiter_BlocksIPAfterMax(t *testing.T) {
	pending.Plan(t, "D6", "failed Basic auth attempts are not limited")
	l := NewFailureLimiter(3, time.Minute)
	t.Cleanup(l.Stop)

	for range 2 {
		l.RecordFailure(sprayer)
	}
	require.False(t, l.Blocked(sprayer), "blocked before the limit")
	l.RecordFailure(sprayer)
	require.True(t, l.Blocked(sprayer), "not blocked at the limit")
	require.False(t, l.Blocked(other), "another IP is blocked")
}

func TestFailureLimiter_WindowExpires(t *testing.T) {
	pending.Plan(t, "D6", "failed Basic auth attempts are not limited")
	l := NewFailureLimiter(2, 50*time.Millisecond)
	t.Cleanup(l.Stop)

	l.RecordFailure(sprayer)
	l.RecordFailure(sprayer)
	require.True(t, l.Blocked(sprayer))

	require.Eventually(t, func() bool { return !l.Blocked(sprayer) }, time.Second, 10*time.Millisecond)
}
