package state

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/google/go-containerregistry/pkg/registry"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

// recordingLocalStore records the store calls applyGroupChanges makes.
type recordingLocalStore struct {
	store.Store
	mu    sync.Mutex
	calls []string
}

func (s *recordingLocalStore) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *recordingLocalStore) Replicate(context.Context, store.Store, []store.Artifact) error {
	s.record("Replicate")
	return nil
}

func (s *recordingLocalStore) Delete(context.Context, []store.Artifact) error {
	s.record("Delete")
	return nil
}

func TestApplyGroupChanges_ReplicatesBeforeDeleting(t *testing.T) {
	pending.Plan(t, "D9", "state replication deletes the old tag before it replicates the new digest")
	local := &recordingLocalStore{}
	moved := Entity{Name: "team/app", Tag: "latest", Digest: "sha256:" + strings.Repeat("b", 64)}
	old := Entity{Name: "team/app", Tag: "latest", Digest: "sha256:" + strings.Repeat("a", 64)}

	require.NoError(t, applyGroupChanges(testContext(), local, nil, changes{replicate: []Entity{moved}, remove: []Entity{old}}))

	first := slices.Index(local.calls, "Replicate")
	require.GreaterOrEqual(t, first, 0, "nothing was replicated: %v", local.calls)
	require.NotContains(t, local.calls[:first], "Delete", "deleted before replicating: %v", local.calls)
}

func TestApplyGroupChanges_SharedLayerNotRefetched(t *testing.T) {
	pending.Plan(t, "D9", "deleting the old digest first removes the layer it shares with the new one, which is then downloaded again")
	shared := []byte("layer shared by both digests")
	sharedDesc := content.NewDescriptorFromBytes("application/vnd.example.layer.v1", shared)
	sharedPath := "/v2/team/app/blobs/" + sharedDesc.Digest.String()
	var sharedGets atomic.Int32
	registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == sharedPath {
			sharedGets.Add(1)
		}
		registryHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	repo, err := orasremote.NewRepository(host + "/team/app")
	require.NoError(t, err)
	repo.PlainHTTP = true
	ctx := testContext()
	require.NoError(t, repo.Push(ctx, sharedDesc, bytes.NewReader(shared)))
	push := func(tag string) ocispec.Descriptor {
		configPayload := []byte(`{"architecture":"amd64","os":"linux","tag":"` + tag + `"}`)
		configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
		require.NoError(t, repo.Push(ctx, configDesc, bytes.NewReader(configPayload)))
		manifest, err := json.Marshal(ocispec.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
			Config: configDesc, Layers: []ocispec.Descriptor{sharedDesc},
		})
		require.NoError(t, err)
		desc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifest)
		require.NoError(t, repo.PushReference(ctx, desc, bytes.NewReader(manifest), tag))
		return desc
	}
	a, b := push("a"), push("b")
	source, err := store.NewRegistryStore(store.RegistryOptions{Endpoint: host, PlainHTTP: true})
	require.NoError(t, err)
	root := t.TempDir()
	local, err := store.NewOCIStore(root)
	require.NoError(t, err)
	old := Entity{Name: "team/app", Tag: "latest", Digest: a.Digest.String()}
	moved := Entity{Name: "team/app", Tag: "latest", Digest: b.Digest.String()}
	require.NoError(t, local.Replicate(ctx, source, []Entity{old}))
	before := sharedGets.Load()

	require.NoError(t, applyGroupChanges(ctx, local, source, changes{replicate: []Entity{moved}, remove: []Entity{old}}))

	require.Equal(t, before, sharedGets.Load(), "the shared layer was downloaded again")
	desc, err := local.Pull(ctx, store.Artifact{Name: "team/app", Tag: "latest"}, store.PullResourceManifest)
	require.NoError(t, err)
	require.Equal(t, b.Digest, desc.Digest)
	_, err = os.Stat(filepath.Join(root, "blobs", a.Digest.Algorithm().String(), a.Digest.Encoded()))
	require.ErrorIs(t, err, os.ErrNotExist, "digest A's manifest must be collected")
}

func TestWithoutWanted(t *testing.T) {
	pending.Plan(t, "D12", "each group deletes what it dropped although another group in the same store still wants it")
	digest := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	latestX := Entity{Name: "team/app", Tag: "latest", Digest: digest("1")}
	cases := []struct {
		name   string
		remove []Entity
		wanted []Entity
		want   []Entity
	}{
		{name: "another group wants the same tag and digest", remove: []Entity{latestX}, wanted: []Entity{latestX}, want: []Entity{}},
		{
			name:   "another tag wants the same digest",
			remove: []Entity{latestX},
			wanted: []Entity{{Name: "team/app", Tag: "v1", Digest: digest("1")}},
			want:   []Entity{},
		},
		{
			name:   "the tag moved to another digest",
			remove: []Entity{latestX},
			wanted: []Entity{{Name: "team/app", Tag: "latest", Digest: digest("2")}},
			want:   []Entity{latestX},
		},
		{
			name:   "same digest under another image",
			remove: []Entity{latestX},
			wanted: []Entity{{Name: "other/app", Tag: "latest", Digest: digest("1")}},
			want:   []Entity{latestX},
		},
		{name: "nothing wants it", remove: []Entity{latestX}, wanted: nil, want: []Entity{latestX}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withoutWanted(tc.remove, tc.wanted)

			require.ElementsMatch(t, tc.want, got)
		})
	}
}
