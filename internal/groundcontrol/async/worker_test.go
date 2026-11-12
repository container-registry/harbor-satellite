package async_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/async"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

type funcJob struct {
	name string
	run  func(ctx context.Context) error
}

func (j funcJob) Name() string                  { return j.name }
func (j funcJob) Run(ctx context.Context) error { return j.run(ctx) }

func untilDone(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func idlePublisher(t *testing.T) *async.Publisher {
	t.Helper()
	cfg := async.DefaultPublisherConfig()
	cfg.PollInterval = 10 * time.Millisecond
	return newPublisher(t, newFakeOutbox(), async.HandlerFunc(func(context.Context, async.OutboxEvent) error { return nil }), cfg)
}

func runWorker(t *testing.T, ctx context.Context, w *async.Worker) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
		return nil
	}
}

func TestNewWorker_RequiresPublisherAndSkipsNilJobs(t *testing.T) {
	_, err := async.NewWorker(nil)
	require.ErrorIs(t, err, fleet.ErrInvalid)

	w, err := async.NewWorker(idlePublisher(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, runWorker(t, ctx, w))
}

func TestWorker_FailingJobStopsTheOthersAndIsReturned(t *testing.T) {
	boom := errors.New("boom")
	stopped := make(chan struct{})
	w, err := async.NewWorker(idlePublisher(t),
		funcJob{name: "watcher", run: func(ctx context.Context) error {
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		}},
		funcJob{name: "failing", run: func(context.Context) error { return boom }},
	)
	require.NoError(t, err)

	err = runWorker(t, context.Background(), w)

	require.ErrorIs(t, err, boom)
	require.ErrorContains(t, err, "failing")
	select {
	case <-stopped:
	default:
		t.Fatal("the sibling job was not cancelled")
	}
}

func TestWorker_CancellationIsNotAnError(t *testing.T) {
	w, err := async.NewWorker(idlePublisher(t),
		funcJob{name: "canceled", run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}},
		funcJob{name: "nil", run: untilDone},
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	require.NoError(t, runWorker(t, ctx, w))
}

// A job that returns nil while the worker still runs would stop every job
// and let `groundcontrol worker` exit 0 with no publisher running.
func TestWorker_JobReturningEarlyIsAnError(t *testing.T) {
	w, err := async.NewWorker(idlePublisher(t),
		funcJob{name: "quitter", run: func(context.Context) error { return nil }},
		funcJob{name: "steady", run: untilDone},
	)
	require.NoError(t, err)

	err = runWorker(t, context.Background(), w)

	require.ErrorIs(t, err, async.ErrJobStopped)
	require.ErrorContains(t, err, "quitter")
}
