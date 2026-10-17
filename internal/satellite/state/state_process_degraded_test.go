package state

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestCanExecuteDegraded(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	writePersistedGroup(t, stateFile, alpineEntity())

	t.Run("missing Harbor password with peers and list", func(t *testing.T) {
		process := NewFetchAndReplicateStateProcess(degradedTestManager(t, true, "80ms"), stateFile, dir, "", nil)
		canExecute, reason := process.CanExecute("", dir, "", "", "")
		require.True(t, canExecute)
		require.Contains(t, reason, "degraded list available")
	})

	t.Run("missing destination still blocked", func(t *testing.T) {
		process := NewFetchAndReplicateStateProcess(degradedTestManager(t, true, "80ms"), stateFile, dir, "", nil)
		canExecute, reason := process.CanExecute("", "", "", "", "")
		require.False(t, canExecute)
		require.Contains(t, reason, "store destination is empty")
	})

	t.Run("peers off still requires Harbor credentials", func(t *testing.T) {
		process := NewFetchAndReplicateStateProcess(degradedTestManager(t, false, ""), stateFile, dir, "", nil)
		canExecute, reason := process.CanExecute("", dir, "", "", "")
		require.False(t, canExecute)
		require.Contains(t, reason, "password is empty")
	})
}

func TestNewFetchAndReplicateStateProcessLoadsBootstrapWhenStateEmpty(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	bootstrap := filepath.Join(dir, config.DefaultDesiredStateFile)
	writePersistedGroup(t, bootstrap, alpineEntity())

	process := NewFetchAndReplicateStateProcess(degradedTestManager(t, true, "80ms"), stateFile, dir, bootstrap, nil)
	require.True(t, process.hasDegradedList())
	require.Equal(t, alpineEntity(), process.stateMap[0].Entities[0])
}

func TestMissingLocally(t *testing.T) {
	ctx := context.Background()
	entity := alpineEntity()

	t.Run("absent artifacts are returned", func(t *testing.T) {
		local := &degradedStubStore{pullErr: errors.New("not found")}
		missing, err := missingLocally(ctx, local, []Entity{entity})
		require.NoError(t, err)
		require.Equal(t, []Entity{entity}, missing)
	})

	t.Run("present artifacts are skipped", func(t *testing.T) {
		local := &degradedStubStore{present: map[string]bool{entityKey(entity): true}}
		missing, err := missingLocally(ctx, local, []Entity{entity})
		require.NoError(t, err)
		require.Empty(t, missing)
	})

	t.Run("tag pointing at a different digest is missing", func(t *testing.T) {
		local := &degradedStubStore{
			present:     map[string]bool{entityKey(entity): true},
			localDigest: "sha256:other",
		}
		missing, err := missingLocally(ctx, local, []Entity{entity})
		require.NoError(t, err)
		require.Equal(t, []Entity{entity}, missing)
	})
}

func TestExecuteDegradedReplication(t *testing.T) {
	entity := alpineEntity()

	t.Run("harbor fetch error with peers and state.json copies missing artifacts", func(t *testing.T) {
		dir := t.TempDir()
		stateFile := filepath.Join(dir, "state.json")
		writePersistedGroup(t, stateFile, entity)
		local := &degradedStubStore{pullErr: errors.New("not found")}
		logs, process := newDegradedProcess(t, dir, stateFile, "", true, local)
		process.fetchRootState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*SatelliteState, error) {
			return nil, errors.New("harbor down")
		}

		require.NoError(t, process.Execute(logs.ctx))
		require.Contains(t, logs.buf.String(), "degraded replication from last-known state")
		require.Equal(t, 1, local.replicateCalls)
		require.Equal(t, 0, local.deleteCalls)
		require.Equal(t, []store.Artifact{entity}, local.lastReplicated)
		require.Equal(t, 80*time.Millisecond, local.harborTimeout)
	})

	t.Run("peers off returns the fetch error", func(t *testing.T) {
		dir := t.TempDir()
		stateFile := filepath.Join(dir, "state.json")
		writePersistedGroup(t, stateFile, entity)
		local := &degradedStubStore{pullErr: errors.New("not found")}
		_, process := newDegradedProcess(t, dir, stateFile, "", false, local)
		process.fetchRootState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*SatelliteState, error) {
			return nil, errors.New("harbor down")
		}

		err := process.Execute(context.Background())
		require.ErrorContains(t, err, "harbor down")
		require.Equal(t, 0, local.replicateCalls)
	})

	t.Run("local pull hit skips replicate", func(t *testing.T) {
		dir := t.TempDir()
		stateFile := filepath.Join(dir, "state.json")
		writePersistedGroup(t, stateFile, entity)
		local := &degradedStubStore{present: map[string]bool{entityKey(entity): true}}
		_, process := newDegradedProcess(t, dir, stateFile, "", true, local)
		process.fetchRootState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*SatelliteState, error) {
			return nil, errors.New("harbor down")
		}

		require.NoError(t, process.Execute(context.Background()))
		require.Equal(t, 0, local.replicateCalls)
		require.Equal(t, 0, local.deleteCalls)
	})

	t.Run("bootstrap file supplies the list", func(t *testing.T) {
		dir := t.TempDir()
		stateFile := filepath.Join(dir, "state.json")
		bootstrap := filepath.Join(dir, config.DefaultDesiredStateFile)
		writePersistedGroup(t, bootstrap, entity)
		local := &degradedStubStore{pullErr: errors.New("not found")}
		_, process := newDegradedProcess(t, dir, stateFile, bootstrap, true, local)
		process.fetchRootState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*SatelliteState, error) {
			return nil, errors.New("harbor down")
		}

		require.NoError(t, process.Execute(context.Background()))
		require.Equal(t, 1, local.replicateCalls)
		require.Equal(t, []store.Artifact{entity}, local.lastReplicated)
		loaded, err := LoadState(stateFile)
		require.NoError(t, err)
		require.NotNil(t, loaded)
		require.Equal(t, []Entity{entity}, loaded.Groups[0].Entities)
	})

	t.Run("empty disk without bootstrap does not invent artifacts", func(t *testing.T) {
		dir := t.TempDir()
		stateFile := filepath.Join(dir, "state.json")
		local := &degradedStubStore{pullErr: errors.New("not found")}
		_, process := newDegradedProcess(t, dir, stateFile, "", true, local)
		process.fetchRootState = func(context.Context, string, string, string, bool, *zerolog.Logger) (*SatelliteState, error) {
			return nil, errors.New("harbor down")
		}

		err := process.Execute(context.Background())
		require.ErrorContains(t, err, "harbor down")
		require.Equal(t, 0, local.replicateCalls)
	})

	t.Run("hung harbor fetch times out then degrades", func(t *testing.T) {
		dir := t.TempDir()
		stateFile := filepath.Join(dir, "state.json")
		writePersistedGroup(t, stateFile, entity)
		local := &degradedStubStore{pullErr: errors.New("not found")}
		_, process := newDegradedProcess(t, dir, stateFile, "", true, local)
		process.fetchRootState = func(ctx context.Context, _ string, _ string, _ string, _ bool, _ *zerolog.Logger) (*SatelliteState, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}

		start := time.Now()
		require.NoError(t, process.Execute(context.Background()))
		require.Less(t, time.Since(start), 2*time.Second)
		require.Equal(t, 1, local.replicateCalls)
	})
}

func TestFetchSatelliteRootStateTimedStopsHungHarbor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	process := &FetchAndReplicateStateProcess{cm: degradedTestManager(t, true, "80ms")}
	log := zerolog.Nop()
	done := make(chan error, 1)
	go func() {
		_, err := process.fetchSatelliteRootStateTimed(context.Background(), srv.URL+"/satellite/state:latest", "user", "pass", true, &log)
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("harbor state fetch did not time out")
	}
}

type degradedLogCapture struct {
	ctx context.Context
	buf *bytes.Buffer
}

func newDegradedProcess(t *testing.T, dir, stateFile, bootstrap string, peers bool, local *degradedStubStore) (degradedLogCapture, *FetchAndReplicateStateProcess) {
	t.Helper()
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	cm := degradedTestManager(t, peers, "80ms")
	process := NewFetchAndReplicateStateProcess(cm, stateFile, dir, bootstrap, &log)
	process.SetStores(local, &degradedStubStore{})
	return degradedLogCapture{
		ctx: context.WithValue(context.Background(), logger.LoggerKey, &log),
		buf: &buf,
	}, process
}

func degradedTestManager(t *testing.T, peers bool, timeout string) *config.ConfigManager {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		StateConfig: config.StateConfig{
			RegistryCredentials: config.RegistryCredentials{
				URL:      "http://source.example.com",
				Username: "source-user",
				Password: "source-password",
			},
			StateURL: "http://source.example.com/satellite/state:latest",
		},
		AppConfig: config.AppConfig{
			UseUnsecure: true,
		},
	}
	if peers {
		cfg.AppConfig.PeerDistribution = config.PeerDistributionConfig{
			Enabled:      true,
			ReachoutSats: "global",
			Timeout:      timeout,
		}
	}
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"token",
		"http://gc:8080",
		false,
		cfg,
	)
	require.NoError(t, err)
	return cm
}

func writePersistedGroup(t *testing.T, path string, entity Entity) {
	t.Helper()
	require.NoError(t, SaveState(path, []StateMap{{
		url:      "http://source.example.com/group-state",
		Entities: []Entity{entity},
	}}, ""))
}

func alpineEntity() Entity {
	return Entity{Name: "alpine", Repository: "library", Tag: "latest", Digest: "sha256:abc"}
}

func entityKey(entity Entity) string {
	return entity.Name + "|" + entity.Tag
}

type degradedStubStore struct {
	mu             sync.Mutex
	present        map[string]bool
	localDigest    string
	pullErr        error
	replicateCalls int
	deleteCalls    int
	lastReplicated []store.Artifact
	harborTimeout  time.Duration
}

func (s *degradedStubStore) Pull(_ context.Context, artifact store.Artifact, _ store.PullResource) (ocispec.Descriptor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.present[entityKey(artifact)] {
		reported := artifact.Digest
		if s.localDigest != "" {
			reported = s.localDigest
		}
		return ocispec.Descriptor{Digest: digest.Digest(reported)}, nil
	}
	if s.pullErr != nil {
		return ocispec.Descriptor{}, s.pullErr
	}
	return ocispec.Descriptor{}, errors.New("not found")
}

func (s *degradedStubStore) Fetch(context.Context, store.Artifact, ocispec.Descriptor, store.PullResource, http.Header) (*http.Response, error) {
	return nil, errors.New("unexpected fetch")
}

func (s *degradedStubStore) Delete(_ context.Context, _ []store.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	return nil
}

func (s *degradedStubStore) Replicate(ctx context.Context, _ store.Store, artifacts []store.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replicateCalls++
	s.lastReplicated = append([]store.Artifact(nil), artifacts...)
	s.harborTimeout = store.HarborTimeoutFrom(ctx)
	return nil
}
