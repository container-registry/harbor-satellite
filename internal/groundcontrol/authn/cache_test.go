package authn

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTTLCache_Expiry(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	c := newTTLCache[string, int](time.Minute, 10)
	c.put("a", 1, t0)

	for _, tc := range []struct {
		name string
		at   time.Time
		hit  bool
	}{
		{"same instant", t0, true},
		{"at the TTL", t0.Add(time.Minute), true},
		{"after the TTL", t0.Add(time.Minute + time.Nanosecond), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := c.get("a", tc.at)
			require.Equal(t, tc.hit, ok)
			if tc.hit {
				require.Equal(t, 1, v)
			}
		})
	}
}

func TestTTLCache_DisabledStoresNothing(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	for _, tc := range []struct {
		name string
		ttl  time.Duration
		max  int
	}{
		{"zero TTL", 0, 10},
		{"zero size", time.Minute, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTTLCache[string, int](tc.ttl, tc.max)
			c.put("a", 1, now)
			_, ok := c.get("a", now)
			require.False(t, ok)
			require.Empty(t, c.entries)
		})
	}
}

func TestTTLCache_FullDropsExpiredThenOldest(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	c := newTTLCache[string, int](time.Minute, 3)
	c.put("old", 1, t0)
	c.put("mid", 2, t0.Add(30*time.Second))
	c.put("new", 3, t0.Add(40*time.Second))

	c.put("d", 4, t0.Add(50*time.Second))
	require.Len(t, c.entries, 3)
	_, ok := c.get("old", t0.Add(50*time.Second))
	require.False(t, ok, "the oldest live entry goes first")

	now := t0.Add(101 * time.Second)
	c.put("e", 5, now)
	require.Len(t, c.entries, 2, "both expired entries are dropped, nothing live is")
	for _, k := range []string{"d", "e"} {
		_, ok := c.get(k, now)
		require.True(t, ok, k)
	}
}

func TestTTLCache_OverwriteDoesNotEvict(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	c := newTTLCache[string, int](time.Minute, 2)
	c.put("a", 1, now)
	c.put("b", 2, now)
	c.put("a", 3, now)

	v, ok := c.get("a", now)
	require.True(t, ok)
	require.Equal(t, 3, v)
	_, ok = c.get("b", now)
	require.True(t, ok)
}

func TestTTLCache_DeleteIf(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	c := newTTLCache[string, int](time.Minute, 10)
	for i := range 6 {
		c.put(fmt.Sprint(i), i, now)
	}
	c.deleteIf(func(v int) bool { return v%2 == 0 })
	require.Len(t, c.entries, 3)
	for _, k := range []string{"1", "3", "5"} {
		_, ok := c.get(k, now)
		require.True(t, ok, k)
	}
}

func TestTTLCache_ConcurrentUseStaysBounded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	c := newTTLCache[int, int](time.Minute, 64)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 500 {
				k := g*1000 + i
				c.put(k, k, now.Add(time.Duration(i)*time.Millisecond))
				c.get(k, now)
				if i%50 == 0 {
					c.deleteIf(func(v int) bool { return v%7 == 0 })
				}
			}
		})
	}
	wg.Wait()
	require.LessOrEqual(t, len(c.entries), 64)
}
