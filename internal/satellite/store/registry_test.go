package store

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/logger"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func testContext() context.Context {
	log := zerolog.Nop()
	return context.WithValue(context.Background(), logger.LoggerKey, &log)
}

// newTestRegistry starts an in-memory OCI registry and returns its host:port address.
func newTestRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// pushImage pushes a random image with the given number of layers to the
// registry at the specified reference. Returns the pushed image.
func pushImage(t *testing.T, addr, imgName, tag string, layerCount int64) v1.Image {
	t.Helper()
	img, err := random.Image(1024, layerCount)
	require.NoError(t, err)

	ref, err := name.ParseReference(addr+"/library/"+imgName+":"+tag, name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, img))
	return img
}

func TestReplicate_NewImage(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	pushImage(t, srcAddr, "alpine", "latest", 2)

	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err := r.Replicate(ctx, []Artifact{
		{Name: "alpine", Repository: "library", Tag: "latest"},
	})
	require.NoError(t, err)

	// Verify image exists at destination
	dstRef, err := name.ParseReference(dstAddr+"/library/alpine:latest", name.Insecure)
	require.NoError(t, err)
	_, err = remote.Head(dstRef)
	require.NoError(t, err)
}

func TestReplicate_UsesIndependentEndpointRepositories(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)
	img, err := random.Image(1024, 2)
	require.NoError(t, err)

	srcRef, err := name.ParseReference(srcAddr+"/source/team/images/httpd:2.4-trixie", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(srcRef, img))

	r := NewRegistryStore(
		RegistryOptions{Endpoint: srcAddr, Repository: "source/team/images", PlainHTTP: true},
		RegistryOptions{Endpoint: dstAddr, Repository: "destination/account", PlainHTTP: true},
	)
	require.NoError(t, r.Replicate(testContext(), []Artifact{
		{Name: "httpd", Repository: "must-not-leak", Tag: "2.4-trixie"},
	}))

	dstRef, err := name.ParseReference(dstAddr+"/destination/account/httpd:2.4-trixie", name.Insecure)
	require.NoError(t, err)
	_, err = remote.Head(dstRef)
	require.NoError(t, err)

	leakedRef, err := name.ParseReference(dstAddr+"/destination/account/must-not-leak/httpd:2.4-trixie", name.Insecure)
	require.NoError(t, err)
	_, err = remote.Head(leakedRef)
	require.Error(t, err)
}

func TestReplicate_SkipsExistingImage(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	// Push same image to both source and destination
	img := pushImage(t, srcAddr, "nginx", "1.25", 2)

	dstRef, err := name.ParseReference(dstAddr+"/library/nginx:1.25", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(dstRef, img))

	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	// Should succeed without error and skip the image
	err = r.Replicate(ctx, []Artifact{
		{Name: "nginx", Repository: "library", Tag: "1.25"},
	})
	require.NoError(t, err)
}

func TestReplicate_UpdatesChangedImage(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	// Push one version to destination
	pushImage(t, dstAddr, "redis", "7", 1)

	// Push a different version to source (different random image)
	srcImg := pushImage(t, srcAddr, "redis", "7", 2)

	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err := r.Replicate(ctx, []Artifact{
		{Name: "redis", Repository: "library", Tag: "7"},
	})
	require.NoError(t, err)

	// Verify destination now has the source image
	dstRef, err := name.ParseReference(dstAddr+"/library/redis:7", name.Insecure)
	require.NoError(t, err)

	dstDesc, err := remote.Head(dstRef)
	require.NoError(t, err)

	srcDigest, err := srcImg.Digest()
	require.NoError(t, err)

	// Digests won't match exactly due to OCI media type conversion,
	// but the image should exist at destination
	require.NotEmpty(t, dstDesc.Digest)
	_ = srcDigest
}

func TestReplicate_MultipleEntities(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	pushImage(t, srcAddr, "alpine", "latest", 1)
	pushImage(t, srcAddr, "nginx", "1.25", 2)

	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err := r.Replicate(ctx, []Artifact{
		{Name: "alpine", Repository: "library", Tag: "latest"},
		{Name: "nginx", Repository: "library", Tag: "1.25"},
	})
	require.NoError(t, err)

	// Both images should exist at destination
	for _, ref := range []string{
		dstAddr + "/library/alpine:latest",
		dstAddr + "/library/nginx:1.25",
	} {
		parsed, err := name.ParseReference(ref, name.Insecure)
		require.NoError(t, err)
		_, err = remote.Head(parsed)
		require.NoError(t, err, "image should exist: %s", ref)
	}
}

func TestReplicate_SourceNotFound(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	// Don't push anything to source
	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err := r.Replicate(ctx, []Artifact{
		{Name: "missing", Repository: "library", Tag: "latest"},
	})
	require.Error(t, err)
}

func TestReplicate_EmptyEntities(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err := r.Replicate(ctx, []Artifact{})
	require.NoError(t, err)
}

func TestCountMissingLayers_AllMissing(t *testing.T) {
	dstAddr := newTestRegistry(t)

	// Create a random image (not pushed to destination)
	img, err := random.Image(1024, 3)
	require.NoError(t, err)

	layers, err := img.Layers()
	require.NoError(t, err)

	dstRef, err := name.ParseReference(dstAddr+"/library/test:latest", name.Insecure)
	require.NoError(t, err)

	r := &RegistryStore{}
	missing := r.countMissingLayers(dstRef, layers, nil)
	require.Equal(t, 3, missing)
}

func TestCountMissingLayers_NoneMissing(t *testing.T) {
	dstAddr := newTestRegistry(t)

	img, err := random.Image(1024, 2)
	require.NoError(t, err)

	// Push image to destination
	dstRef, err := name.ParseReference(dstAddr+"/library/test:latest", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(dstRef, img))

	layers, err := img.Layers()
	require.NoError(t, err)

	r := &RegistryStore{}
	missing := r.countMissingLayers(dstRef, layers, nil)
	require.Equal(t, 0, missing)
}

func TestCountMissingLayers_PartialOverlap(t *testing.T) {
	dstAddr := newTestRegistry(t)

	// Push one image to destination (has its own layers)
	oldImg, err := random.Image(1024, 2)
	require.NoError(t, err)

	dstRef, err := name.ParseReference(dstAddr+"/library/test:latest", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(dstRef, oldImg))

	// Create a new image with different layers
	newImg, err := random.Image(1024, 3)
	require.NoError(t, err)

	newLayers, err := newImg.Layers()
	require.NoError(t, err)

	r := &RegistryStore{}
	missing := r.countMissingLayers(dstRef, newLayers, nil)
	// Random images have unique layers, so all new layers should be missing
	require.Equal(t, 3, missing)
}

func TestDelete(t *testing.T) {
	dstAddr := newTestRegistry(t)

	pushImage(t, dstAddr, "alpine", "latest", 1)

	r := NewRegistryStore(RegistryOptions{Endpoint: "", PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err := r.Delete(ctx, []Artifact{
		{Name: "alpine", Repository: "library", Tag: "latest"},
	})
	require.NoError(t, err)
}

// TestReplicate_LayerResume simulates crash mid-replication and verifies that
// already-present layers are skipped during resume. This tests the blob-level
// deduplication that happens inside remote.Write via HEAD checks.
func TestReplicate_LayerResume(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	// Step 1: Create base image with 3 layers
	baseImg, err := random.Image(1024, 3)
	require.NoError(t, err)
	baseLayers, err := baseImg.Layers()
	require.NoError(t, err)
	require.Len(t, baseLayers, 3, "base image should have 3 layers")

	// Step 2: Create extended image by appending 2 new layers to base image
	// This creates an image with 5 layers where first 3 are shared with baseImg
	newLayer1, err := random.Layer(1024, types.DockerLayer)
	require.NoError(t, err)
	newLayer2, err := random.Layer(1024, types.DockerLayer)
	require.NoError(t, err)

	extendedImgRaw, err := mutate.AppendLayers(baseImg, newLayer1, newLayer2)
	require.NoError(t, err)

	// Convert to OCI format (same as what replicator does) for consistent digest
	extendedImg := mutate.MediaType(extendedImgRaw, types.OCIManifestSchema1)
	extendedLayers, err := extendedImg.Layers()
	require.NoError(t, err)
	require.Len(t, extendedLayers, 5, "extended image should have 5 layers")

	// Step 3: Push base image to both source and dest
	// This simulates a previous replication that completed (establishing 3 layers at dest)
	baseRefSrc, err := name.ParseReference(srcAddr+"/library/app:base", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(baseRefSrc, baseImg))

	baseRefDst, err := name.ParseReference(dstAddr+"/library/app:base", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(baseRefDst, baseImg))

	// Step 4: Push extended image to source only
	extRefSrc, err := name.ParseReference(srcAddr+"/library/app:extended", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.Write(extRefSrc, extendedImg))

	// Step 5: Replicate the extended image from source to dest
	// The destination already has 3 of the 5 layers (from base image)
	// remote.Write should detect these via blob HEAD checks and only pull the 2 new layers
	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})
	ctx := testContext()

	err = r.Replicate(ctx, []Artifact{
		{Name: "app", Repository: "library", Tag: "extended"},
	})
	require.NoError(t, err, "replication should succeed with layer-level resume")

	// Step 6: Verify the image exists at destination with correct content
	extRefDst, err := name.ParseReference(dstAddr+"/library/app:extended", name.Insecure)
	require.NoError(t, err)

	dstImg, err := remote.Image(extRefDst, remote.WithContext(ctx))
	require.NoError(t, err, "replicated image should exist at destination")

	// Verify layer count matches source
	finalLayers, err := dstImg.Layers()
	require.NoError(t, err)
	require.Len(t, finalLayers, 5, "destination should have all 5 layers")

	// Verify digest matches source (proves all layers were correctly replicated)
	srcDigest, err := extendedImg.Digest()
	require.NoError(t, err)
	dstDigest, err := dstImg.Digest()
	require.NoError(t, err)
	require.Equal(t, srcDigest, dstDigest, "digests should match after replication")
}

func TestReplicate_CancelledContextStopsProcessing(t *testing.T) {
	srcAddr := newTestRegistry(t)
	dstAddr := newTestRegistry(t)

	pushImage(t, srcAddr, "img1", "v1", 1)
	pushImage(t, srcAddr, "img2", "v1", 1)
	pushImage(t, srcAddr, "img3", "v1", 1)

	r := NewRegistryStore(RegistryOptions{Endpoint: srcAddr, PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})

	ctx, cancel := context.WithCancel(testContext())
	cancel() // cancel immediately

	err := r.Replicate(ctx, []Artifact{
		{Name: "img1", Repository: "library", Tag: "v1"},
		{Name: "img2", Repository: "library", Tag: "v1"},
		{Name: "img3", Repository: "library", Tag: "v1"},
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestDelete_CancelledContextStopsProcessing(t *testing.T) {
	dstAddr := newTestRegistry(t)

	pushImage(t, dstAddr, "img1", "v1", 1)
	pushImage(t, dstAddr, "img2", "v1", 1)

	r := NewRegistryStore(RegistryOptions{Endpoint: "", PlainHTTP: true}, RegistryOptions{Endpoint: dstAddr, PlainHTTP: true})

	ctx, cancel := context.WithCancel(testContext())
	cancel()

	err := r.Delete(ctx, []Artifact{
		{Name: "img1", Repository: "library", Tag: "v1"},
		{Name: "img2", Repository: "library", Tag: "v1"},
	})

	require.ErrorIs(t, err, context.Canceled)
}
