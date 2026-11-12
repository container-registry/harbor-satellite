package async

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var (
	_ Job = (*Publisher)(nil)
	_ Job = (*RegistryProbe)(nil)
	_ Job = (*StaleDetector)(nil)
	_ Job = (*SessionExpirer)(nil)
)

// Job is a long-running part of the async plane. A job retries its own
// failures and returns only when ctx ends or it cannot go on at all, such
// as with an invalid configuration.
type Job interface {
	Name() string
	Run(ctx context.Context) error
}

// Worker runs the publisher and further jobs until ctx ends. It runs
// embedded in `groundcontrol serve --worker=embedded` or alone as
// `groundcontrol worker`; several workers may run against one database.
type Worker struct {
	jobs []Job
}

func NewWorker(publisher *Publisher, jobs ...Job) (*Worker, error) {
	if publisher == nil {
		return nil, fmt.Errorf("%w: the worker needs a publisher", fleet.ErrInvalid)
	}
	all := []Job{publisher}
	for _, j := range jobs {
		if j != nil {
			all = append(all, j)
		}
	}
	return &Worker{jobs: all}, nil
}

// Run stops every job when ctx ends or one job returns, and returns the
// first error.
func (w *Worker) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for _, j := range w.jobs {
		wg.Go(func() {
			err := j.Run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				once.Do(func() { first = fmt.Errorf("%s: %w", j.Name(), err) })
			}
			cancel()
		})
	}
	wg.Wait()
	return first
}
