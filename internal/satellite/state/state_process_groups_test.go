package state

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

func TestExecute_ImageStaysWhileAnotherGroupWantsIt(t *testing.T) {
	ctx := testContext()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	shared, _ := pushRetagTestImage(t, host, "team/app", "shared", []byte("layer of the image both groups want"))
	other, _ := pushRetagTestImage(t, host, "team/tool", "other", []byte("layer only group A wants later"))

	groupA := host + "/satellite/group-state/group-a/state:latest"
	groupB := host + "/satellite/group-state/group-b/state:latest"
	configStateRef := host + "/satellite/config-state/edge/state:latest"
	satelliteStateRef := host + "/satellite/satellite-state/sat/state:latest"
	pushStateArtifact(t, configStateRef, config.Config{AppConfig: config.AppConfig{UseUnsecure: true}})
	pushStateArtifact(t, satelliteStateRef, SatelliteState{
		States: []string{"http://" + groupA, "http://" + groupB},
		Config: "http://" + configStateRef,
	})
	sharedImage := Artifact{Repository: "team/app", Tags: []string{"latest"}, Digest: shared.Digest.String(), Type: "image"}
	pushStateArtifact(t, groupA, State{Registry: server.URL, Artifacts: []Artifact{sharedImage}})
	pushStateArtifact(t, groupB, State{Registry: server.URL, Artifacts: []Artifact{sharedImage}})

	root := t.TempDir()
	local, err := store.NewOCIStore(root)
	require.NoError(t, err)
	dir := t.TempDir()
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"", "http://gc.invalid", false,
		&config.Config{
			StateConfig: config.StateConfig{
				RegistryCredentials: config.RegistryCredentials{URL: config.URL(server.URL), Username: "robot", Password: "secret"},
				StateURL:            "http://" + satelliteStateRef,
			},
			AppConfig: config.AppConfig{UseUnsecure: true},
		},
	)
	require.NoError(t, err)
	nop := zerolog.Nop()
	process := NewFetchAndReplicateStateProcess(cm, "", root, "", &nop)
	process.SetStores(local, nil)
	tagged := store.Artifact{Name: "team/app", Tag: "latest"}

	require.NoError(t, process.Execute(ctx))
	desc, err := local.Pull(ctx, tagged, store.PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, shared.Digest, desc.Digest)

	pushStateArtifact(t, groupA, State{Registry: server.URL, Artifacts: []Artifact{
		{Repository: "team/tool", Tags: []string{"latest"}, Digest: other.Digest.String(), Type: "image"},
	}})
	require.NoError(t, process.Execute(ctx))

	desc, err = local.Pull(ctx, tagged, store.PullResourceManifest)
	require.NoError(t, err, "group B still wants team/app:latest")
	require.Equal(t, shared.Digest, desc.Digest)
}

// groupsHarness runs state replication for one satellite against a local
// registry that serves both the images and the state artifacts.
type groupsHarness struct {
	t         *testing.T
	server    *httptest.Server
	host      string
	stateFile string
	root      string
	cm        *config.ConfigManager
	local     *store.OCIStore
	process   *FetchAndReplicateStateProcess
}

func newGroupsHarness(t *testing.T) *groupsHarness {
	t.Helper()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	h := &groupsHarness{t: t, server: server, host: strings.TrimPrefix(server.URL, "http://"), root: t.TempDir()}
	dir := t.TempDir()
	h.stateFile = filepath.Join(dir, "state.json")
	pushStateArtifact(t, h.host+"/satellite/config-state/edge/state:latest", config.Config{AppConfig: config.AppConfig{UseUnsecure: true}})
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"", "http://gc.invalid", false,
		&config.Config{
			StateConfig: config.StateConfig{
				RegistryCredentials: config.RegistryCredentials{URL: config.URL(server.URL), Username: "robot", Password: "secret"},
				StateURL:            "http://" + h.host + "/satellite/satellite-state/sat/state:latest",
			},
			AppConfig: config.AppConfig{UseUnsecure: true},
		},
	)
	require.NoError(t, err)
	h.cm = cm
	h.local, err = store.NewOCIStore(h.root)
	require.NoError(t, err)
	h.process = h.newProcess(h.local)
	return h
}

func (h *groupsHarness) newProcess(local store.Store) *FetchAndReplicateStateProcess {
	nop := zerolog.Nop()
	process := NewFetchAndReplicateStateProcess(h.cm, h.stateFile, h.root, "", &nop)
	process.SetStores(local, nil)
	return process
}

func (h *groupsHarness) groupRef(group string) string {
	return h.host + "/satellite/group-state/" + group + "/state:latest"
}

// assign makes groups the satellite's groups.
func (h *groupsHarness) assign(groups ...string) {
	states := make([]string, 0, len(groups))
	for _, g := range groups {
		states = append(states, "http://"+h.groupRef(g))
	}
	pushStateArtifact(h.t, h.host+"/satellite/satellite-state/sat/state:latest", SatelliteState{
		States: states,
		Config: "http://" + h.host + "/satellite/config-state/edge/state:latest",
	})
}

func (h *groupsHarness) setGroup(group string, artifacts ...Artifact) {
	pushStateArtifact(h.t, h.groupRef(group), State{Registry: h.server.URL, Artifacts: artifacts})
}

func (h *groupsHarness) image(repository, tag, layer string) (Artifact, ocispec.Descriptor) {
	manifest, _ := pushRetagTestImage(h.t, h.host, repository, tag, []byte(layer))
	return Artifact{Repository: repository, Tags: []string{tag}, Digest: manifest.Digest.String(), Type: "image"}, manifest
}

func (h *groupsHarness) requirePresent(name, tag string, want ocispec.Descriptor) {
	h.t.Helper()
	desc, err := h.local.Pull(testContext(), store.Artifact{Name: name, Tag: tag}, store.PullResourceManifest)
	require.NoError(h.t, err, "%s:%s must stay", name, tag)
	require.Equal(h.t, want.Digest, desc.Digest)
}

func (h *groupsHarness) requireGone(name, tag string) {
	h.t.Helper()
	_, err := h.local.Pull(testContext(), store.Artifact{Name: name, Tag: tag}, store.PullResourceManifest)
	require.ErrorIs(h.t, err, errdef.ErrNotFound, "%s:%s must be deleted", name, tag)
}

func TestExecute_RemovedGroupContentIsDeleted(t *testing.T) {
	h := newGroupsHarness(t)
	app, appManifest := h.image("team/app", "latest", "layer of group A")
	tool, _ := h.image("team/tool", "latest", "layer of group B")
	h.setGroup("group-a", app)
	h.setGroup("group-b", tool)
	h.assign("group-a", "group-b")
	require.NoError(t, h.process.Execute(testContext()))
	h.requirePresent("team/tool", "latest", ocispec.Descriptor{Digest: digestOf(t, tool)})

	h.assign("group-a")
	require.NoError(t, h.process.Execute(testContext()))

	h.requireGone("team/tool", "latest")
	h.requirePresent("team/app", "latest", appManifest)
	persisted, err := LoadState(h.stateFile)
	require.NoError(t, err)
	require.Empty(t, persisted.Retired, "nothing is left to delete")
}

func TestExecute_RemovedGroupKeepsWhatAnotherGroupWants(t *testing.T) {
	h := newGroupsHarness(t)
	app, appManifest := h.image("team/app", "latest", "layer both groups want")
	tool, _ := h.image("team/tool", "latest", "layer only group B wants")
	h.setGroup("group-a", app)
	h.setGroup("group-b", app, tool)
	h.assign("group-a", "group-b")
	require.NoError(t, h.process.Execute(testContext()))

	h.assign("group-a")
	require.NoError(t, h.process.Execute(testContext()))

	h.requirePresent("team/app", "latest", appManifest)
	h.requireGone("team/tool", "latest")
}

func TestExecute_DroppedTagGoesAlthoughAnotherTagKeepsItsDigest(t *testing.T) {
	h := newGroupsHarness(t)
	v1, manifest := h.image("team/app", "v1", "layer under two tags")
	_, _ = h.image("team/app", "v2", "layer under two tags")
	both := v1
	both.Tags = []string{"v1", "v2"}
	h.setGroup("group-a", both)
	h.assign("group-a")
	require.NoError(t, h.process.Execute(testContext()))
	h.requirePresent("team/app", "v1", manifest)

	onlyV2 := v1
	onlyV2.Tags = []string{"v2"}
	h.setGroup("group-a", onlyV2)
	require.NoError(t, h.process.Execute(testContext()))

	h.requireGone("team/app", "v1")
	h.requirePresent("team/app", "v2", manifest)
}

// failingDeleteStore fails the first Delete, as a full disk or a lost BYO
// registry would.
type failingDeleteStore struct {
	*store.OCIStore
	failures atomic.Int32
}

func (s *failingDeleteStore) Delete(ctx context.Context, artifacts []store.Artifact) error {
	if s.failures.Add(-1) >= 0 {
		return errors.New("disk on fire")
	}
	return s.OCIStore.Delete(ctx, artifacts)
}

func TestExecute_RemovedGroupIsDeletedAfterARestartWhenTheDeleteFailed(t *testing.T) {
	h := newGroupsHarness(t)
	app, appManifest := h.image("team/app", "latest", "layer of group A")
	tool, _ := h.image("team/tool", "latest", "layer of group B")
	h.setGroup("group-a", app)
	h.setGroup("group-b", tool)
	h.assign("group-a", "group-b")
	failing := &failingDeleteStore{OCIStore: h.local}
	failing.failures.Store(1)
	first := h.newProcess(failing)
	require.NoError(t, first.Execute(testContext()))

	h.assign("group-a")
	require.Error(t, first.Execute(testContext()))
	persisted, err := LoadState(h.stateFile)
	require.NoError(t, err)
	require.Len(t, persisted.Groups, 1)
	require.NotEmpty(t, persisted.Retired, "the failed delete must survive a restart")

	restarted := h.newProcess(h.local)
	require.NoError(t, restarted.Execute(testContext()))

	h.requireGone("team/tool", "latest")
	h.requirePresent("team/app", "latest", appManifest)
}

func digestOf(t *testing.T, a Artifact) digest.Digest {
	t.Helper()
	d, err := digest.Parse(a.Digest)
	require.NoError(t, err)
	return d
}
