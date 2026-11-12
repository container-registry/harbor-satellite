package store

import (
	"sync"

	digest "github.com/opencontainers/go-digest"
)

// RefCounter counts open readers per digest so CollectGarbage skips content
// that is being served, without a store lock held for the body's lifetime.
type RefCounter struct {
	mu     sync.Mutex
	counts map[digest.Digest]int
}

func NewRefCounter() *RefCounter {
	return &RefCounter{counts: map[digest.Digest]int{}}
}

// Acquire marks d in use until release is called; release is idempotent.
func (r *RefCounter) Acquire(d digest.Digest) (release func()) {
	r.mu.Lock()
	r.counts[d]++
	r.mu.Unlock()
	return sync.OnceFunc(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.counts[d] <= 1 {
			delete(r.counts, d)
			return
		}
		r.counts[d]--
	})
}

func (r *RefCounter) InUse(d digest.Digest) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[d] > 0
}
