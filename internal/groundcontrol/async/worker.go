package async

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
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
type Worker struct{}

func NewWorker(publisher *Publisher, jobs ...Job) (*Worker, error) {
	return nil, notimpl.Err("2.4 worker")
}

// Run stops every job when ctx ends or one job returns, and returns the
// first error.
func (w *Worker) Run(ctx context.Context) error {
	return notimpl.Err("2.4 worker run")
}
