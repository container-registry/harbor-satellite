package store

import (
	"context"
	"sync"
)

// storeLock is a reader/writer lock with the same preference as sync.RWMutex:
// many readers, or one writer, and a waiting writer blocks new readers so a
// publish is not starved by a stream of pulls.
//
// sync.RWMutex is not used. Its Lock call cannot be abandoned, and once a
// writer is queued every later RLock waits until that writer acquires and
// releases the lock. A peer publish has to return when its context ends while
// Fetch still holds a read lock for an open body, and that abandoned wait must
// not keep blocking readers.
//
// Waiters select on a broadcast channel. sync.Cond cannot also wait on
// ctx.Done, so closing and replacing the channel is the condition variable.
type storeLock struct {
	mu      sync.Mutex
	readers int
	writer  bool
	waiting int
	changed chan struct{}
}

func (l *storeLock) rLock() {
	l.mu.Lock()
	for l.writer || l.waiting > 0 {
		changed := l.changedLocked()
		l.mu.Unlock()
		<-changed
		l.mu.Lock()
	}
	l.readers++
	l.mu.Unlock()
}

func (l *storeLock) rUnlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.readers == 0 {
		panic("store lock read unlock without read lock")
	}
	l.readers--
	if l.readers == 0 {
		l.notifyLocked()
	}
}

func (l *storeLock) lock(ctx context.Context) error {
	l.mu.Lock()
	if err := ctx.Err(); err != nil {
		l.mu.Unlock()
		return err
	}
	// Count this writer before it waits, so a reader that arrives now blocks
	// behind it instead of keeping the publish queued forever.
	l.waiting++
	for l.writer || l.readers > 0 {
		changed := l.changedLocked()
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			l.mu.Lock()
			l.waiting--
			l.notifyLocked()
			l.mu.Unlock()
			return ctx.Err()
		case <-changed:
		}
		l.mu.Lock()
		if err := ctx.Err(); err != nil {
			l.waiting--
			l.notifyLocked()
			l.mu.Unlock()
			return err
		}
	}
	l.waiting--
	l.writer = true
	l.mu.Unlock()
	return nil
}

func (l *storeLock) unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.writer {
		panic("store lock unlock without write lock")
	}
	l.writer = false
	l.notifyLocked()
}

// changedLocked returns the channel the next state change will close.
// l.mu is held by the caller.
func (l *storeLock) changedLocked() <-chan struct{} {
	if l.changed == nil {
		l.changed = make(chan struct{})
	}
	return l.changed
}

// notifyLocked wakes every waiter. l.mu is held by the caller.
func (l *storeLock) notifyLocked() {
	if l.changed == nil {
		return
	}
	close(l.changed)
	l.changed = make(chan struct{})
}
