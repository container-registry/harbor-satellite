package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/google/go-containerregistry/pkg/registry"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

// digestSource is a registry holding two manifests of team/app, tagged "a"
// and "b", that share one layer and differ in their config.
type digestSource struct {
	store   *RegistryStore
	a, b    ocispec.Descriptor
	aConfig ocispec.Descriptor
	shared  ocispec.Descriptor
}

func newDigestSource(t *testing.T) digestSource {
	t.Helper()
	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	repo, err := orasremote.NewRepository(address + "/team/app")
	require.NoError(t, err)
	repo.PlainHTTP = true
	ctx := context.Background()
	shared := []byte("layer shared by a and b")
	sharedDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", shared)
	require.NoError(t, repo.Push(ctx, sharedDesc, bytes.NewReader(shared)))
	push := func(tag string) (manifestDesc, configDesc ocispec.Descriptor) {
		configPayload := []byte(`{"architecture":"amd64","os":"linux","tag":"` + tag + `"}`)
		configDesc = content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
		require.NoError(t, repo.Push(ctx, configDesc, bytes.NewReader(configPayload)))
		manifest, err := json.Marshal(ocispec.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
			Config: configDesc, Layers: []ocispec.Descriptor{sharedDesc},
		})
		require.NoError(t, err)
		manifestDesc = content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifest)
		require.NoError(t, repo.PushReference(ctx, manifestDesc, bytes.NewReader(manifest), tag))
		return manifestDesc, configDesc
	}
	src := digestSource{shared: sharedDesc}
	src.a, src.aConfig = push("a")
	src.b, _ = push("b")
	src.store, err = NewRegistryStore(RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)
	return src
}

// latestAt is team/app:latest as state replication names it while the tag
// points to manifest.
func latestAt(manifest ocispec.Descriptor) Artifact {
	return Artifact{Name: "team/app", Tag: "latest", Digest: manifest.Digest.String()}
}

func blobExists(t *testing.T, root string, desc ocispec.Descriptor) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, "blobs", desc.Digest.Algorithm().String(), desc.Digest.Encoded()))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	require.NoError(t, err)
	return true
}

func TestOCIStoreDelete_KeepsMovedTag(t *testing.T) {
	pending.Plan(t, "D9", "Delete untags whatever the tag resolves to, also after a newer replication moved it")
	src := newDigestSource(t)
	local, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.a)}))
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.b)}))

	require.NoError(t, local.Delete(ctx, []Artifact{latestAt(src.a)}))

	desc, err := local.Pull(ctx, Artifact{Name: "team/app", Tag: "latest"}, PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, src.b.Digest, desc.Digest)
}

func TestOCIStoreDelete_CollectsMovedDigest(t *testing.T) {
	pending.Plan(t, "D9", "Delete collects garbage only when it untagged something")
	src := newDigestSource(t)
	root := t.TempDir()
	local, err := NewOCIStore(root)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.a)}))
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.b)}))
	require.True(t, blobExists(t, root, src.a), "the moved tag leaves digest A in the layout until it is collected")

	require.NoError(t, local.Delete(ctx, []Artifact{latestAt(src.a)}))

	require.False(t, blobExists(t, root, src.a), "digest A's manifest must be collected")
	require.False(t, blobExists(t, root, src.aConfig), "digest A's config must be collected")
	require.True(t, blobExists(t, root, src.b), "digest B is still tagged")
	require.True(t, blobExists(t, root, src.shared), "digest B still uses the shared layer")
}

func TestOCIStoreDelete_CollectsWhenTagIsGone(t *testing.T) {
	pending.Plan(t, "D9", "Delete returns early on a missing tag and skips collection")
	src := newDigestSource(t)
	root := t.TempDir()
	local, err := NewOCIStore(root)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.a)}))
	// As after a crash between untag and collection, or a recovered index.
	require.NoError(t, local.store.Untag(ctx, latestAt(src.a).Reference()))
	require.True(t, blobExists(t, root, src.a), "untagging alone leaves digest A in the layout")

	require.NoError(t, local.Delete(ctx, []Artifact{latestAt(src.a)}))

	require.False(t, blobExists(t, root, src.a), "digest A's manifest must be collected")
	require.False(t, blobExists(t, root, src.shared), "no manifest references the layer any more")
}

func TestOCIStoreDelete_RemovesUnchangedTagAndItsContent(t *testing.T) {
	src := newDigestSource(t)
	root := t.TempDir()
	local, err := NewOCIStore(root)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.a)}))

	require.NoError(t, local.Delete(ctx, []Artifact{latestAt(src.a)}))

	_, err = local.Pull(ctx, Artifact{Name: "team/app", Tag: "latest"}, PullResourceManifest)
	require.ErrorIs(t, err, errdef.ErrNotFound)
	require.False(t, blobExists(t, root, src.a))
	require.False(t, blobExists(t, root, src.shared))
}

func TestRegistryStoreDelete_KeepsMovedTagsManifest(t *testing.T) {
	pending.Plan(t, "D9", "Delete removes whatever manifest the tag resolves to, also after a newer replication moved it")
	src := newDigestSource(t)
	var (
		mu      sync.Mutex
		deletes []string
	)
	destination := registry.New()
	// The in-memory registry keeps a tag when its manifest is deleted by
	// digest, so only the DELETE requests show what was removed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deletes = append(deletes, r.URL.Path)
			mu.Unlock()
		}
		destination.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	local, err := NewRegistryStore(RegistryOptions{Endpoint: strings.TrimPrefix(server.URL, "http://"), PlainHTTP: true})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.a)}))
	require.NoError(t, local.Replicate(ctx, src.store, []Artifact{latestAt(src.b)}))

	require.NoError(t, local.Delete(ctx, []Artifact{latestAt(src.a)}))

	mu.Lock()
	defer mu.Unlock()
	require.NotContains(t, deletes, "/v2/team/app/manifests/"+src.b.Digest.String(), "the tag moved to digest B, which must stay")
}
