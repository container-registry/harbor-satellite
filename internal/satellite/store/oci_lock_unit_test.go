package store

import (
	"context"
	"testing"
	"time"
)

func TestStoreLockAbandonedWriterDoesNotBlockReaders(t *testing.T) {
	var lock storeLock
	lock.rLock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := lock.lock(ctx)
	if err == nil {
		t.Fatal("writer acquired the lock while a reader held it")
	}

	acquired := make(chan struct{})
	go func() {
		lock.rLock()
		close(acquired)
		lock.rUnlock()
	}()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("abandoned writer still blocked a new reader")
	}
	lock.rUnlock()
}

func TestStoreLockWaitingWriterBlocksNewReaders(t *testing.T) {
	var lock storeLock
	lock.rLock()

	acquired := make(chan struct{})
	go func() {
		if err := lock.lock(context.Background()); err != nil {
			t.Errorf("lock: %v", err)
			return
		}
		close(acquired)
		lock.unlock()
	}()

	// The writer increments its wait count before blocking, so a reader that
	// starts after that call is queued cannot pass it. Give the writer the
	// scheduler once, then prove the reader stays blocked until the first
	// reader unlocks.
	waitUntilWriterQueued(t, &lock)

	readerDone := make(chan struct{})
	go func() {
		lock.rLock()
		lock.rUnlock()
		close(readerDone)
	}()
	select {
	case <-readerDone:
		t.Fatal("reader ran ahead of the waiting writer")
	case <-time.After(50 * time.Millisecond):
	}

	lock.rUnlock()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("writer did not acquire after the reader unlocked")
	}
	select {
	case <-readerDone:
	case <-time.After(time.Second):
		t.Fatal("reader did not run after the writer unlocked")
	}
}

func waitUntilWriterQueued(t *testing.T, lock *storeLock) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		lock.mu.Lock()
		queued := lock.waiting > 0
		lock.mu.Unlock()
		if queued {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("writer did not queue")
		}
		time.Sleep(time.Millisecond)
	}
}
