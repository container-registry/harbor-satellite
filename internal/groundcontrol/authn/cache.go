package authn

import (
	"sync"
	"time"
)

// ttlCache is a bounded map whose entries expire after ttl. When full it
// drops expired entries first and then the oldest one, so its size never
// exceeds max.
type ttlCache[K comparable, V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	entries map[K]cacheEntry[V]
}

type cacheEntry[V any] struct {
	value  V
	stored time.Time
}

func newTTLCache[K comparable, V any](ttl time.Duration, maxEntries int) *ttlCache[K, V] {
	return &ttlCache[K, V]{ttl: ttl, max: maxEntries, entries: map[K]cacheEntry[V]{}}
}

func (c *ttlCache[K, V]) get(k K, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || c.ttl <= 0 || now.Sub(e.stored) > c.ttl {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *ttlCache[K, V]) put(k K, v V, now time.Time) {
	if c.ttl <= 0 || c.max <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[k]; !ok && len(c.entries) >= c.max {
		c.evict(now)
	}
	c.entries[k] = cacheEntry[V]{value: v, stored: now}
}

func (c *ttlCache[K, V]) evict(now time.Time) {
	var oldestKey K
	var oldest time.Time
	first := true
	for k, e := range c.entries {
		if now.Sub(e.stored) > c.ttl {
			delete(c.entries, k)
			continue
		}
		if first || e.stored.Before(oldest) {
			oldestKey, oldest, first = k, e.stored, false
		}
	}
	if len(c.entries) >= c.max && !first {
		delete(c.entries, oldestKey)
	}
}

// deleteIf drops every entry whose value matches.
func (c *ttlCache[K, V]) deleteIf(match func(V) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if match(e.value) {
			delete(c.entries, k)
		}
	}
}
