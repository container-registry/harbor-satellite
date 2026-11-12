package state

import (
	"context"
	"path/filepath"
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

func TestQuotaSkipKeepsThePreviousImage(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	oldImage := alpineEntity()
	updated := oldImage
	updated.Digest = "sha256:def"
	writePersistedGroup(t, stateFile, oldImage)

	require.NoError(t, budget.Apply(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "10", Window: "sync"},
	}))
	t.Cleanup(func() {
		require.NoError(t, budget.Apply(config.NetworkConfig{}))
	})

	local := &degradedStubStore{}
	_, process := newDegradedProcess(t, dir, stateFile, "", false, local)
	process.measureMissing = func(context.Context, store.Store, store.Store, store.Artifact) (int64, error) {
		return 100, nil
	}
	process.fetchGroupState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*StateReader, error) {
		state := &State{Artifacts: []Artifact{{
			Name:       updated.Name,
			Repository: updated.Repository,
			Tags:       []string{updated.Tag},
			Digest:     updated.Digest,
		}}}
		var reader StateReader = state
		return &reader, nil
	}
	process.cm.With(config.SetNetworkConfig(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "10", Window: "sync"},
	}))

	log := zerolog.Nop()
	result := process.processGroupState(context.Background(), 0, "source-user", "source-password", true, local, &degradedStubStore{}, &log)
	require.Nil(t, result.Error)
	require.Zero(t, local.replicateCalls)
	require.Zero(t, local.deleteCalls)
	require.Equal(t, []Entity{oldImage}, process.stateMap[0].Entities)
}

func TestReplacementDeleteUsesTheOldDigest(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	oldImage := alpineEntity()
	updated := oldImage
	updated.Digest = "sha256:def"
	writePersistedGroup(t, stateFile, oldImage)

	require.NoError(t, budget.Apply(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "10", Window: "sync"},
	}))
	t.Cleanup(func() {
		require.NoError(t, budget.Apply(config.NetworkConfig{}))
	})

	local := &degradedStubStore{}
	_, process := newDegradedProcess(t, dir, stateFile, "", false, local)
	process.measureMissing = func(context.Context, store.Store, store.Store, store.Artifact) (int64, error) {
		return 4, nil
	}
	process.fetchGroupState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*StateReader, error) {
		state := &State{Artifacts: []Artifact{{
			Name:       updated.Name,
			Repository: updated.Repository,
			Tags:       []string{updated.Tag},
			Digest:     updated.Digest,
		}}}
		var reader StateReader = state
		return &reader, nil
	}
	process.cm.With(config.SetNetworkConfig(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "10", Window: "sync"},
	}))

	log := zerolog.Nop()
	result := process.processGroupState(context.Background(), 0, "source-user", "source-password", true, local, &degradedStubStore{}, &log)
	require.Nil(t, result.Error)
	require.Equal(t, []store.Artifact{{
		Name:       oldImage.Name,
		Repository: oldImage.Repository,
		Digest:     oldImage.Digest,
	}}, local.lastDeleted)
	require.Equal(t, []Entity{updated}, process.stateMap[0].Entities)
}
