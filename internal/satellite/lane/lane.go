// Package lane runs background work, such as pull-through prefetch, with
// bounded concurrency and a bounded queue (plan item 5.3).
package lane

import (
	"context"
	"errors"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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

type Lane struct{}

func New(name string, opts Options) (*Lane, error) {
	return nil, notimpl.Err("5.3 lane")
}

// Acquire blocks until a slot is free and returns the function that frees it.
func (l *Lane) Acquire(ctx context.Context) (release func(), err error) {
	return nil, notimpl.Err("5.3 lane acquire")
}

// Submit queues job without waiting for it; ErrLaneFull when the queue is full.
func (l *Lane) Submit(ctx context.Context, job func(ctx context.Context) error) error {
	return notimpl.Err("5.3 lane submit")
}

// Close stops accepting jobs and waits for running and queued ones until ctx ends.
func (l *Lane) Close(ctx context.Context) error {
	return notimpl.Err("5.3 lane close")
}
