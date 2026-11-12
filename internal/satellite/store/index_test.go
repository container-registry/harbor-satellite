package store

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
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/logger"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/google/go-containerregistry/pkg/registry"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasremote "oras.land/oras-go/v2/registry/remote"
)

// edgeContext carries a silent logger, which Replicate expects.
func edgeContext(t *testing.T) context.Context {
	t.Helper()
	nop := zerolog.Nop()
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), logger.LoggerKey, &nop), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// edgeUpstream is an in-memory registry; wrap may intercept requests.
func edgeUpstream(t *testing.T, wrap func(next http.Handler) http.Handler) (string, *RegistryStore) {
	t.Helper()
	handler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	if wrap != nil {
		handler = wrap(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	source, err := NewRegistryStore(RegistryOptions{Endpoint: address, PlainHTTP: true})
	require.NoError(t, err)
	return address, source
}

// pushEdgeImage pushes a manifest with one config and the given layers.
func pushEdgeImage(t *testing.T, address, repository, tag string, layers ...[]byte) (ocispec.Descriptor, []ocispec.Descriptor) {
	t.Helper()
	ctx := context.Background()
	repo, err := orasremote.NewRepository(address + "/" + repository)
	require.NoError(t, err)
	repo.PlainHTTP = true
	configPayload := []byte(`{"architecture":"amd64","os":"linux","repository":"` + repository + `"}`)
	configDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configPayload)
	require.NoError(t, repo.Push(ctx, configDesc, bytes.NewReader(configPayload)))
	layerDescs := make([]ocispec.Descriptor, 0, len(layers))
	for _, layer := range layers {
		desc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageLayer, layer)
		require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(layer)))
		layerDescs = append(layerDescs, desc)
	}
	manifestPayload, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		Config: configDesc, Layers: layerDescs,
	})
	require.NoError(t, err)
	manifestDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, manifestPayload)
	require.NoError(t, repo.PushReference(ctx, manifestDesc, bytes.NewReader(manifestPayload), tag))
	return manifestDesc, layerDescs
}

func edgeBlobPath(root string, d digest.Digest) string {
	return filepath.Join(root, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Encoded())
}

func readIndex(t *testing.T, root string) ocispec.Index {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ocispec.ImageIndexFile))
	require.NoError(t, err)
	var index ocispec.Index
	require.NoError(t, json.Unmarshal(data, &index))
	return index
}

func TestOpenOCIStore_RecoversTornIndex(t *testing.T) {
	pending.Plan(t, "5.1", "a torn index.json stops the satellite from starting")
	ctx := edgeContext(t)
	address, source := edgeUpstream(t, nil)
	pushEdgeImage(t, address, "team/app", "v1", []byte("layer"))
	artifact := Artifact{Name: "team/app", Tag: "v1"}
	root := t.TempDir()
	first, err := NewOCIStore(root)
	require.NoError(t, err)
	require.NoError(t, first.Replicate(ctx, source, []Artifact{artifact}))

	indexPath := filepath.Join(root, ocispec.ImageIndexFile)
	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(indexPath, data[:len(data)/2], 0o600))
	_, err = NewOCIStore(root)
	require.ErrorContains(t, err, "failed to decode index file")

	recovered, report, err := OpenOCIStore(root, OCIStoreOptions{AtomicIndex: true, RecoverIndex: true})
	require.NoError(t, err)
	require.True(t, report.IndexRecovered)
	require.FileExists(t, report.QuarantinedPath)
	require.NotEqual(t, indexPath, report.QuarantinedPath)

	require.NoError(t, recovered.Replicate(ctx, source, []Artifact{artifact}))
	_, err = recovered.Pull(ctx, artifact, PullResourceManifest)
	require.NoError(t, err)
}

func TestWriteIndexAtomic_KeepsOldIndexOnFailure(t *testing.T) {
	pending.Plan(t, "5.1", "oras-go writes index.json in place")
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	index := func(tag string) ocispec.Index {
		return ocispec.Index{
			Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageIndex,
			Manifests: []ocispec.Descriptor{{
				MediaType: ocispec.MediaTypeImageManifest, Digest: digest.FromString(tag), Size: 1,
				Annotations: map[string]string{ocispec.AnnotationRefName: "team/app:" + tag},
			}},
		}
	}

	require.NoError(t, WriteIndexAtomic(root, index("v1")))
	require.Equal(t, index("v1"), readIndex(t, root))

	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(root, 0o700)) })
	require.Error(t, WriteIndexAtomic(root, index("v2")))
	require.Equal(t, index("v1"), readIndex(t, root))
}

func TestOCIStore_AtomicIndexAfterMutations(t *testing.T) {
	pending.Plan(t, "5.1", "oras-go writes index.json in place")
	ctx := edgeContext(t)
	address, source := edgeUpstream(t, nil)
	pushEdgeImage(t, address, "team/app", "v1", []byte("layer one"))
	pushEdgeImage(t, address, "team/app", "v2", []byte("layer two"))
	root := t.TempDir()
	s, _, err := OpenOCIStore(root, OCIStoreOptions{AtomicIndex: true})
	require.NoError(t, err)

	v1, v2 := Artifact{Name: "team/app", Tag: "v1"}, Artifact{Name: "team/app", Tag: "v2"}
	require.NoError(t, s.Replicate(ctx, source, []Artifact{v1, v2}))
	require.NoError(t, s.Delete(ctx, []Artifact{v1}))

	var refs []string
	for _, desc := range readIndex(t, root).Manifests {
		if ref, ok := desc.Annotations[ocispec.AnnotationRefName]; ok {
			refs = append(refs, ref)
		}
	}
	require.Equal(t, []string{"team/app:v2"}, refs)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, e := range entries {
		require.True(t, slices.Contains([]string{ocispec.ImageBlobsDir, "ingest", ocispec.ImageIndexFile, ocispec.ImageLayoutFile}, e.Name()),
			"unexpected %s left in the store root", e.Name())
	}
}

func TestOCIStore_FetchDoesNotBlockTag(t *testing.T) {
	pending.Plan(t, "5.1", "Fetch holds the store-wide read lock until the body is closed")
	ctx := edgeContext(t)
	address, source := edgeUpstream(t, nil)
	_, layers := pushEdgeImage(t, address, "team/served", "v1", []byte("served layer"))
	pushEdgeImage(t, address, "team/new", "v1", []byte("new layer"))
	s, err := NewOCIStore(t.TempDir())
	require.NoError(t, err)
	served := Artifact{Name: "team/served", Tag: "v1"}
	require.NoError(t, s.Replicate(ctx, source, []Artifact{served}))

	blob := Artifact{Name: "team/served", Digest: layers[0].Digest.String()}
	desc, err := s.Pull(ctx, blob, PullResourceBlob)
	require.NoError(t, err)
	reader, err := s.Fetch(ctx, blob, desc, PullResourceBlob, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Body.Close() })

	replicated := make(chan error, 1)
	go func() { replicated <- s.Replicate(ctx, source, []Artifact{{Name: "team/new", Tag: "v1"}}) }()
	select {
	case err := <-replicated:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.NoError(t, reader.Body.Close())
		<-replicated
		t.Fatal("Replicate waited for an open reader to close")
	}
}
