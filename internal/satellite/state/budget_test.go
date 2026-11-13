package state

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/budget"
	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
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
		measureMissing: func(ctx context.Context, _, _ store.Store, _ store.Artifact) (int64, error) {
			require.False(t, budget.Metered(ctx))
			return 11, nil
		},
	}
	log := zerolog.Nop()
	copied, err := process.replicateEach(context.Background(), local, local, []Entity{alpineEntity()}, &log)
	require.NoError(t, err)
	require.Empty(t, copied)
	require.Equal(t, 0, local.replicateCalls)

	budget.Ensure().BeginSync()
	process.measureMissing = func(ctx context.Context, _, _ store.Store, _ store.Artifact) (int64, error) {
		require.False(t, budget.Metered(ctx))
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

func TestReplicateEachDoesNotSizeWhenTheQuotaIsDisabled(t *testing.T) {
	dir := t.TempDir()
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"token",
		"http://gc:8080",
		false,
		&config.Config{AppConfig: config.AppConfig{GroundControlURL: "http://gc:8080"}},
	)
	require.NoError(t, err)
	cm.With(config.SetNetworkConfig(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: false, Limit: "lots", Window: "month"},
	}))
	require.NoError(t, budget.Apply(config.NetworkConfig{}))
	t.Cleanup(func() {
		require.NoError(t, budget.Apply(config.NetworkConfig{}))
	})

	var calls int
	local := &degradedStubStore{}
	process := &FetchAndReplicateStateProcess{
		cm: cm,
		measureMissing: func(context.Context, store.Store, store.Store, store.Artifact) (int64, error) {
			calls++
			return 100, nil
		},
	}
	log := zerolog.Nop()
	copied, err := process.replicateEach(context.Background(), local, local, []Entity{alpineEntity()}, &log)
	require.NoError(t, err)
	require.Equal(t, []Entity{alpineEntity()}, copied)
	require.Equal(t, 0, calls)
	require.Equal(t, 1, local.replicateCalls)
}

func TestDirectDeliveryQuotaSkipStaysPending(t *testing.T) {
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	img, err := random.Image(2048, 1)
	require.NoError(t, err)
	ref, err := name.ParseReference(host+"/library/alpine:latest", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, img))
	digest, err := img.Digest()
	require.NoError(t, err)

	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	oldImage := alpineEntity()
	writePersistedGroup(t, stateFile, oldImage)

	require.NoError(t, budget.Apply(config.NetworkConfig{
		Quota: config.NetworkQuotaConfig{Enabled: true, Limit: "10", Window: "sync"},
	}))
	t.Cleanup(func() {
		require.NoError(t, budget.Apply(config.NetworkConfig{}))
	})
	budget.Ensure().BeginSync()

	local := &degradedStubStore{}
	_, process := newDegradedProcess(t, dir, stateFile, "", false, local)
	process.directDeliverer = NewDirectDeliverer(t.TempDir(), "", "", host, true)
	process.measureMissing = func(context.Context, store.Store, store.Store, store.Artifact) (int64, error) {
		return 1, nil
	}
	updated := oldImage
	updated.Digest = digest.String()
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
	require.Equal(t, []Entity{oldImage}, process.stateMap[0].Entities)
}
