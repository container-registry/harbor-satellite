package root

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/telemetry"
	"github.com/stretchr/testify/require"
)

func executeServe(t *testing.T, deps serveDependencies, args ...string) error {
	t.Helper()
	cmd := newServeCommand(deps)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.ExecuteContext(t.Context())
}

func TestServeWorkerFlag_RejectsUnknown(t *testing.T) {
	t.Parallel()
	called := false
	deps := serveDependencies{loadEnvironment: func() error {
		called = true
		return errors.New("stop after the flag check")
	}}

	err := executeServe(t, deps, "--worker", "sidecar")

	require.ErrorContains(t, err, `invalid --worker "sidecar"`)
	require.False(t, called, "an invalid flag stops serve before it loads anything")
	require.ErrorContains(t, executeServe(t, deps, "--worker", WorkerOff), "stop after the flag check")
	require.True(t, called)
}

func TestServe_StartsWithHarborUnreachable(t *testing.T) {
	reachedServer := errors.New("reached the server")
	deps := serveDependencies{
		loadEnvironment: func() error { return nil },
		checkHealth:     func(context.Context) error { return errors.New("harbor unreachable") },
		migrate:         func(context.Context) error { return nil },
		newServer: func(context.Context) (*server.ServerResult, error) {
			return nil, reachedServer
		},
	}

	require.ErrorIs(t, executeServe(t, deps), reachedServer)
}

// A job that returns while serve runs makes /health unhealthy; jobs that
// return because serve shuts down do not.
func TestServe_StoppedJobMarksHealthUnhealthy(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := &server.ServerResult{AppServer: &server.Server{}, Server: &http.Server{Addr: "127.0.0.1:0", ReadHeaderTimeout: time.Second}}
	failed := make(chan struct{})
	deps := serveDependencies{
		loadEnvironment: func() error { return nil },
		migrate:         func(context.Context) error { return nil },
		newServer:       func(context.Context) (*server.ServerResult, error) { return result, nil },
		jobs: func(context.Context, bool) ([]job, error) {
			return []job{
				{name: "async worker", run: func(context.Context) error {
					defer close(failed)
					return errors.New("outbox store gone")
				}},
				{name: "registry probe", run: func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				}},
			}, nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- runServe(ctx, io.Discard, deps, true) }()

	receive(t, failed)
	require.Eventually(t, func() bool { return len(result.AppServer.StoppedJobs()) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"async worker"}, result.AppServer.StoppedJobs())
	cancel()
	require.NoError(t, receive(t, done))
	require.Equal(t, []string{"async worker"}, result.AppServer.StoppedJobs(), "the probe returned on shutdown")
}

func TestListenMetrics_PortInUseStopsStartup(t *testing.T) {
	metrics, err := telemetry.NewServer(telemetry.Options{})
	require.NoError(t, err)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer taken.Close()

	_, err = listenMetrics(t.Context(), taken.Addr().String(), metrics)
	require.ErrorContains(t, err, "METRICS_ADDR")
}
