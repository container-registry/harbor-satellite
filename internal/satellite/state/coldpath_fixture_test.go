package state

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

// countingRegistry is an in-memory registry that counts manifest and blob
// requests by method and path.
type countingRegistry struct {
	*httptest.Server
	host string
	mu   sync.Mutex
	hits map[string]int
}

func newCountingRegistry(t *testing.T) *countingRegistry {
	t.Helper()
	handler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	r := &countingRegistry{hits: map[string]int{}}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/manifests/") || strings.Contains(req.URL.Path, "/blobs/") {
			r.mu.Lock()
			r.hits[req.Method+" "+req.URL.Path]++
			r.mu.Unlock()
		}
		handler.ServeHTTP(w, req)
	}))
	t.Cleanup(r.Close)
	r.host = strings.TrimPrefix(r.URL, "http://")
	return r
}

func (r *countingRegistry) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = map[string]int{}
}

// count sums the requests of method whose path contains part.
func (r *countingRegistry) count(method, part string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for key, hits := range r.hits {
		m, path, _ := strings.Cut(key, " ")
		if m == method && strings.Contains(path, part) {
			n += hits
		}
	}
	return n
}

// coldPathSatellite is a satellite "sat" whose groups and config "edge" are
// published in a countingRegistry.
type coldPathSatellite struct {
	cm      *config.ConfigManager
	process *FetchAndReplicateStateProcess
	// groupStates are the group state references by group name, the
	// *Repo fields the repository paths the registry sees.
	groupStates                             map[string]string
	groupStateDigests                       map[string]string
	satelliteStateRepo, configStateRepo     string
	groupStateRepos                         map[string]string
	satelliteStateDigest, configStateDigest string
}

// newColdPathSatellite publishes one group state per entry of groups and a
// config state with appConfig, and builds the state process for them. It
// sets no stores, so Execute builds a local OCI store and a Harbor store.
func newColdPathSatellite(t *testing.T, reg *countingRegistry, appConfig config.AppConfig, groups map[string][]Artifact) *coldPathSatellite {
	t.Helper()
	appConfig.UseUnsecure = true
	s := &coldPathSatellite{
		groupStates:        map[string]string{},
		groupStateDigests:  map[string]string{},
		groupStateRepos:    map[string]string{},
		satelliteStateRepo: "satellite/satellite-state/sat/state",
		configStateRepo:    "satellite/config-state/edge/state",
	}
	satelliteState := reg.host + "/" + s.satelliteStateRepo + ":latest"
	configState := reg.host + "/" + s.configStateRepo + ":latest"
	var stateURLs []string
	for name, artifacts := range groups {
		repo := "satellite/group-state/" + name + "/state"
		ref := reg.host + "/" + repo + ":latest"
		s.groupStateRepos[name] = repo
		s.groupStates[name] = ref
		pushStateArtifact(t, ref, State{Registry: reg.URL, Artifacts: artifacts})
		digest, err := crane.Digest(ref, crane.Insecure)
		require.NoError(t, err)
		s.groupStateDigests[name] = digest
		stateURLs = append(stateURLs, "http://"+ref)
	}
	pushStateArtifact(t, configState, config.Config{AppConfig: appConfig})
	pushStateArtifact(t, satelliteState, SatelliteState{States: stateURLs, Config: "http://" + configState})
	var err error
	s.satelliteStateDigest, err = crane.Digest(satelliteState, crane.Insecure)
	require.NoError(t, err)
	s.configStateDigest, err = crane.Digest(configState, crane.Insecure)
	require.NoError(t, err)

	dir := t.TempDir()
	s.cm, err = config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"", "http://gc.invalid", false,
		&config.Config{
			StateConfig: config.StateConfig{
				RegistryCredentials: config.RegistryCredentials{URL: config.URL(reg.URL), Username: "robot", Password: "secret"},
				StateURL:            "http://" + satelliteState,
			},
			AppConfig: appConfig,
		},
	)
	require.NoError(t, err)
	nop := zerolog.Nop()
	s.process = NewFetchAndReplicateStateProcess(s.cm, "", t.TempDir(), &nop)
	return s
}

// pushImage pushes a one-layer image and returns the group state entry for it.
func pushImage(t *testing.T, reg *countingRegistry, repository string, layer []byte) Artifact {
	t.Helper()
	manifest, _ := pushRetagTestImage(t, reg.host, repository, "latest", layer)
	return Artifact{Repository: repository, Tags: []string{"latest"}, Digest: manifest.Digest.String(), Type: "image"}
}

// blockingStore is a local store whose Replicate waits for release and
// records how many replications run at once.
type blockingStore struct {
	release       chan struct{}
	mu            sync.Mutex
	running, peak int
}

func (b *blockingStore) Pull(context.Context, store.Artifact, store.PullResource) (ocispec.Descriptor, error) {
	return ocispec.Descriptor{}, errdef.ErrNotFound
}

func (b *blockingStore) Fetch(context.Context, store.Artifact, ocispec.Descriptor, store.PullResource, http.Header) (*http.Response, error) {
	return nil, errdef.ErrNotFound
}

func (b *blockingStore) Replicate(ctx context.Context, _ store.Store, artifacts []store.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	b.mu.Lock()
	b.running++
	b.peak = max(b.peak, b.running)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.running--
		b.mu.Unlock()
	}()
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingStore) Delete(context.Context, []store.Artifact) error {
	return nil
}

func (b *blockingStore) counts() (running, peak int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running, b.peak
}
