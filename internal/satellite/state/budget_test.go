package state

import (
	"context"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/budget"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestReplicateEachSkipsTheImageThatDoesNotFit(t *testing.T) {
	require.NoError(t, budget.Apply(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "10", Window: "sync"},
	}))
	t.Cleanup(func() {
		require.NoError(t, budget.Apply(config.NetworkConfig{}))
	})

	local := &degradedStubStore{}
	process := &FetchAndReplicateStateProcess{
		measureMissing: func(context.Context, store.Store, store.Store, store.Artifact) (int64, error) {
			return 11, nil
		},
	}
	log := zerolog.Nop()
	copied, err := process.replicateEach(context.Background(), local, local, []Entity{alpineEntity()}, &log)
	require.NoError(t, err)
	require.Empty(t, copied)
	require.Equal(t, 0, local.replicateCalls)

	budget.Ensure().BeginSync()
	process.measureMissing = func(context.Context, store.Store, store.Store, store.Artifact) (int64, error) {
		return 4, nil
	}
	copied, err = process.replicateEach(context.Background(), local, local, []Entity{alpineEntity()}, &log)
	require.NoError(t, err)
	require.Equal(t, []Entity{alpineEntity()}, copied)
	require.Equal(t, 1, local.replicateCalls)
	require.Equal(t, []store.Artifact{alpineEntity()}, local.lastReplicated)
}
