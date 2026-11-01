package store

import (
	"context"
	"sync"
)

// storeLock excludes writers from readers. Writer acquire returns when the
// attempt context ends, so a peer publish can give up while Fetch still holds
// a read lock for an open local response.
//
// A waiting writer blocks new readers. That keeps a publish from being starved
// by a stream of pulls, and it matches the previous RWMutex behavior.
type storeLock struct {
	mu      sync.Mutex
	readers int
	writer  bool
	waiting int
	waiters []chan struct{}
}

func (l *storeLock) rLock() {
	l.mu.Lock()
	if !l.writer && l.waiting == 0 {
		l.readers++
		l.mu.Unlock()
		return
	}
	ch := make(chan struct{}, 1)
	l.waiters = append(l.waiters, ch)
	for l.writer || l.waiting > 0 {
		l.mu.Unlock()
		<-ch
		l.mu.Lock()
	}
	l.remove(ch)
	l.readers++
	l.mu.Unlock()
}

func (l *storeLock) rUnlock() {
	l.mu.Lock()
	if l.readers == 0 {
		l.mu.Unlock()
		panic("store lock read unlock without read lock")
	}
	l.readers--
	if l.readers == 0 {
		l.signal()
	}
	l.mu.Unlock()
}

func (l *storeLock) lock(ctx context.Context) error {
	l.mu.Lock()
	if err := ctx.Err(); err != nil {
		l.mu.Unlock()
		return err
	}
	if !l.writer && l.readers == 0 {
		l.writer = true
		l.mu.Unlock()
		return nil
	}
	l.waiting++
	ch := make(chan struct{}, 1)
	l.waiters = append(l.waiters, ch)
	for l.writer || l.readers > 0 {
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			l.mu.Lock()
			l.abandon(ch)
			l.mu.Unlock()
			return ctx.Err()
		case <-ch:
			l.mu.Lock()
			if err := ctx.Err(); err != nil {
				l.abandon(ch)
				l.mu.Unlock()
				return err
			}
		}
	}
	l.waiting--
	l.remove(ch)
	l.writer = true
	l.mu.Unlock()
	return nil
}

func (l *storeLock) unlock() {
	l.mu.Lock()
	if !l.writer {
		l.mu.Unlock()
		panic("store lock unlock without write lock")
	}
	l.writer = false
	l.signal()
	l.mu.Unlock()
}

// abandon drops a writer wait. l.mu is held by the caller.
func (l *storeLock) abandon(ch chan struct{}) {
	l.waiting--
	l.remove(ch)
	l.signal()
}

// signal wakes current waiters. l.mu is held by the caller.
func (l *storeLock) signal() {
	for _, ch := range l.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (l *storeLock) remove(ch chan struct{}) {
	for i, waiter := range l.waiters {
		if waiter == ch {
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			return
		}
	}
}
