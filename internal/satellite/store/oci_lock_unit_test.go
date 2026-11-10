package store

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
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

func TestStoreLockQueuedReaderProceedsWhenWriterCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var lock storeLock
		lock.rLock()

		ctx, cancel := context.WithCancel(context.Background())
		writerDone := make(chan error, 1)
		go func() {
			writerDone <- lock.lock(ctx)
		}()
		synctest.Wait()

		acquired := make(chan struct{})
		go func() {
			lock.rLock()
			close(acquired)
		}()
		synctest.Wait()

		cancel()
		synctest.Wait()

		select {
		case err := <-writerDone:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("writer returned %v, want context.Canceled", err)
			}
		default:
			t.Fatal("writer did not return after cancel")
		}
		select {
		case <-acquired:
		default:
			t.Fatal("queued reader stayed blocked after the publish was canceled")
		}
		lock.mu.Lock()
		readers := lock.readers
		lock.mu.Unlock()
		if readers != 2 {
			t.Fatalf("readers = %d, want 2 while the original read lock is still held", readers)
		}
		lock.rUnlock()
		lock.rUnlock()
	})
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
