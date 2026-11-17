// Package lane runs background work, such as pull-through prefetch, with
// bounded concurrency and a bounded queue (plan item 5.3).
package lane

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
)

var (
	ErrLaneFull   = errors.New("lane queue is full")
	ErrLaneClosed = errors.New("lane is closed")
)

type Options struct {
	// Concurrency is how many jobs run at once.
	Concurrency int
	// QueueSize is how many submitted jobs may wait; Submit fails beyond it.
	QueueSize int
}

type job struct {
	ctx context.Context
	run func(ctx context.Context) error
}

type Lane struct {
	name  string
	slots chan struct{}
	queue chan job
	// capacity counts running and waiting jobs, so a job a worker has not
	// picked up yet does not make Submit fail early.
	capacity int

	mu       sync.Mutex
	accepted int
	closed   bool

	stop    context.Context
	cancel  context.CancelFunc
	workers sync.WaitGroup
}

func New(name string, opts Options) (*Lane, error) {
	if opts.Concurrency < 1 {
		return nil, fmt.Errorf("lane %s: concurrency must be at least 1, got %d", name, opts.Concurrency)
	}
	if opts.QueueSize < 0 {
		return nil, fmt.Errorf("lane %s: queue size must not be negative, got %d", name, opts.QueueSize)
	}
	stop, cancel := context.WithCancel(context.Background())
	l := &Lane{
		name:     name,
		slots:    make(chan struct{}, opts.Concurrency),
		capacity: opts.Concurrency + opts.QueueSize,
		stop:     stop,
		cancel:   cancel,
	}
	l.queue = make(chan job, l.capacity)
	for range opts.Concurrency {
		l.workers.Go(l.work)
	}
	return l, nil
}

func (l *Lane) work() {
	for j := range l.queue {
		l.slots <- struct{}{}
		l.runJob(j)
		<-l.slots
		l.mu.Lock()
		l.accepted--
		l.mu.Unlock()
	}
}

func (l *Lane) runJob(j job) {
	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	stopWatch := context.AfterFunc(l.stop, cancel)
	defer stopWatch()
	defer func() {
		if r := recover(); r != nil {
			logger.FromContext(ctx).Error().Str("lane", l.name).Interface("panic", r).Msg("lane job panicked")
		}
	}()
	if err := j.run(ctx); err != nil && ctx.Err() == nil {
		logger.FromContext(ctx).Warn().Str("lane", l.name).Err(err).Msg("lane job failed")
	}
}

// Acquire blocks until a slot is free and returns the function that frees it.
// It shares the slots with submitted jobs.
func (l *Lane) Acquire(ctx context.Context) (release func(), err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.stop.Done():
		return nil, ErrLaneClosed
	case l.slots <- struct{}{}:
		return sync.OnceFunc(func() { <-l.slots }), nil
	}
}

// Submit queues job without waiting for it; ErrLaneFull when the queue is
// full. The job runs with ctx's values, and its context ends with ctx or
// when the lane closes.
func (l *Lane) Submit(ctx context.Context, run func(ctx context.Context) error) error {
	if run == nil {
		return errors.New("lane: job is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrLaneClosed
	}
	if l.accepted >= l.capacity {
		return ErrLaneFull
	}
	l.accepted++
	l.queue <- job{ctx: ctx, run: run}
	return nil
}

// Close stops accepting jobs, cancels the context of running and queued
// jobs, and waits until they returned or ctx ends. Background work is best
// effort: whatever a cancelled prefetch did not finish, the next pull redoes.
func (l *Lane) Close(ctx context.Context) error {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	l.cancel()

	done := make(chan struct{})
	go func() {
		l.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("lane %s: close: %w", l.name, ctx.Err())
	}
}
