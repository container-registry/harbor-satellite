package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

func TestExecute_RetaggedImageStaysPullable(t *testing.T) {
	pending.Plan(t, "D9", "re-tagged image is missing while the new digest replicates")

	ctx := testContext()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")

	manifestA, layerA := pushRetagTestImage(t, host, "team/app", "a", []byte("layer only in digest A"))
	manifestB, _ := pushRetagTestImage(t, host, "team/app", "b", []byte("layer only in digest B"))

	groupStateRef := host + "/satellite/group-state/edge/state:latest"
	configStateRef := host + "/satellite/config-state/edge/state:latest"
	satelliteStateRef := host + "/satellite/satellite-state/sat/state:latest"
	pushStateArtifact(t, configStateRef, config.Config{AppConfig: config.AppConfig{UseUnsecure: true}})
	pushStateArtifact(t, satelliteStateRef, SatelliteState{
		States: []string{"http://" + groupStateRef},
		Config: "http://" + configStateRef,
	})
	pushGroupState := func(manifest ocispec.Descriptor) {
		pushStateArtifact(t, groupStateRef, State{
			Registry:  server.URL,
			Artifacts: []Artifact{{Repository: "team/app", Tags: []string{"latest"}, Digest: manifest.Digest.String(), Type: "image"}},
		})
	}

	root := t.TempDir()
	ociStore, err := store.NewOCIStore(root)
	require.NoError(t, err)
	local := &probingStore{OCIStore: ociStore}

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
	process := NewFetchAndReplicateStateProcess(cm, "", root, &nop)
	process.SetStores(local, nil)

	tagged := store.Artifact{Name: "team/app", Tag: "latest"}
	pushGroupState(manifestA)
	require.NoError(t, process.Execute(ctx))
	desc, err := ociStore.Pull(ctx, tagged, store.PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, manifestA.Digest, desc.Digest)

	var (
		mu       sync.Mutex
		failures []string
	)
	recordPull := func(stage string) {
		if _, err := ociStore.Pull(ctx, tagged, store.PullResourceManifest); err != nil {
			mu.Lock()
			failures = append(failures, stage+": "+err.Error())
			mu.Unlock()
		}
	}
	local.probe = recordPull

	stop := make(chan struct{})
	var looped atomic.Int64
	pulling := make(chan struct{})
	go func() {
		defer close(pulling)
		for {
			select {
			case <-stop:
				return
			default:
				recordPull("concurrent pull")
				looped.Add(1)
			}
		}
	}()

	pushGroupState(manifestB)
	err = process.Execute(ctx)
	close(stop)
	<-pulling
	require.NoError(t, err)
	require.Positive(t, looped.Load())

	mu.Lock()
	require.Empty(t, failures, "the tag must resolve throughout the move from digest A to digest B")
	mu.Unlock()
	desc, err = ociStore.Pull(ctx, tagged, store.PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, manifestB.Digest, desc.Digest)
	for _, collected := range []ocispec.Descriptor{manifestA, layerA} {
		_, err := os.Stat(filepath.Join(root, "blobs", collected.Digest.Algorithm().String(), collected.Digest.Encoded()))
		require.ErrorIs(t, err, os.ErrNotExist, "digest A content %s must be garbage-collected", collected.Digest)
	}
}

// probingStore checks the tag between the store calls state replication
// makes, so a window where the tag is absent cannot slip between iterations
// of the concurrent pull loop.
type probingStore struct {
	*store.OCIStore
	probe func(stage string)
}

func (p *probingStore) Replicate(ctx context.Context, source store.Store, artifacts []store.Artifact) error {
	err := p.OCIStore.Replicate(ctx, source, artifacts)
	if p.probe != nil {
		p.probe("after Replicate")
	}
	return err
}

func (p *probingStore) Delete(ctx context.Context, artifacts []store.Artifact) error {
	err := p.OCIStore.Delete(ctx, artifacts)
	if p.probe != nil {
		p.probe("after Delete")
	}
	return err
}

func pushRetagTestImage(t *testing.T, host, repository, tag string, layer []byte) (manifest, layerDesc ocispec.Descriptor) {
	t.Helper()
	repo, err := orasremote.NewRepository(host + "/" + repository)
	require.NoError(t, err)
	repo.PlainHTTP = true
	layerDesc = content.NewDescriptorFromBytes("application/vnd.example.layer.v1", layer)
	configPayload := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		Config: configDesc, Layers: []ocispec.Descriptor{layerDesc},
	})
	require.NoError(t, err)
	manifest = content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	ctx := context.Background()
	for _, blob := range []struct {
		desc    ocispec.Descriptor
		payload []byte
	}{{layerDesc, layer}, {configDesc, configPayload}} {
		if err := repo.Push(ctx, blob.desc, bytes.NewReader(blob.payload)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
			require.NoError(t, err)
		}
	}
	require.NoError(t, repo.PushReference(ctx, manifest, bytes.NewReader(manifestPayload), tag))
	return manifest, layerDesc
}

func pushStateArtifact(t *testing.T, ref string, state any) {
	t.Helper()
	payload, err := json.Marshal(state)
	require.NoError(t, err)
	img, err := crane.Image(map[string][]byte{"artifacts.json": payload})
	require.NoError(t, err)
	require.NoError(t, crane.Push(img, ref, crane.Insecure))
}
