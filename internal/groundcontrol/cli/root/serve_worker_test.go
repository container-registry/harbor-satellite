package root

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/server"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
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
	pending.Plan(t, "2.6", "serve refuses to start while Harbor is unreachable")
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
