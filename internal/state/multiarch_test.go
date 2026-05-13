package state

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
	"zotregistry.dev/zot/pkg/api"
	"zotregistry.dev/zot/pkg/api/config"
)

// TestReplicate_PreservesMultiArchIndex pushes a multi-arch index to the source,
// runs the replicator, and asserts the destination preserves the index media type
// rather than flattening to a single platform.
func TestReplicate_PreservesMultiArchIndex(t *testing.T) {
	_, srcAddr := newTestRegistry(t)
	_, dstAddr := newTestRegistry(t)

	// Build a 3-platform OCI image index.
	idx, err := random.Index(1024, 2, 3)
	require.NoError(t, err)

	srcRef, err := name.ParseReference(srcAddr+"/library/multiarch:latest", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(srcRef, idx))

	// Sanity: source must actually be an index for this test to be meaningful.
	srcDesc, err := remote.Get(srcRef)
	require.NoError(t, err)
	require.True(t, srcDesc.MediaType.IsIndex(),
		"fixture broken: source is not an index, got %s", srcDesc.MediaType)

	r := NewBasicReplicator("", "", srcAddr, dstAddr, "", "", true)
	ctx := testContext()

	require.NoError(t, r.Replicate(ctx, []Entity{
		{Name: "multiarch", Repository: "library", Tag: "latest"},
	}))

	dstRef, err := name.ParseReference(dstAddr+"/library/multiarch:latest", name.Insecure)
	require.NoError(t, err)
	dstDesc, err := remote.Get(dstRef)
	require.NoError(t, err)

	require.True(t, dstDesc.MediaType.IsIndex(),
		"expected destination to preserve multi-arch index, got %s", dstDesc.MediaType)

	// The destination index has the same number of platform manifests as the source,
	// but child media types are rewritten to OCI for Zot compatibility so the
	// top-level digest will differ from source. Assert structural preservation
	// rather than byte equality.
	dstIdx, err := remote.Index(dstRef)
	require.NoError(t, err)
	dstManifest, err := dstIdx.IndexManifest()
	require.NoError(t, err)
	require.Len(t, dstManifest.Manifests, 3, "expected 3 platform manifests preserved")
}

// TestReplicate_PreservesMultiArchIndex_ZotDestination exercises the same multi-arch
// preservation against the embedded Zot registry that satellite ships with, rather
// than the in-memory registry used above. Catches Zot-specific media-type or storage
// behavior that the in-memory variant cannot.
func TestReplicate_PreservesMultiArchIndex_ZotDestination(t *testing.T) {
	_, srcAddr := newTestRegistry(t)
	dstAddr := startZotForTest(t, t.TempDir())

	idx, err := random.Index(1024, 2, 3)
	require.NoError(t, err)

	srcRef, err := name.ParseReference(srcAddr+"/library/multiarch:latest", name.Insecure)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(srcRef, idx))

	srcDesc, err := remote.Get(srcRef)
	require.NoError(t, err)
	require.True(t, srcDesc.MediaType.IsIndex(),
		"fixture broken: source is not an index, got %s", srcDesc.MediaType)

	r := NewBasicReplicator("", "", srcAddr, dstAddr, "", "", true)
	ctx := testContext()

	require.NoError(t, r.Replicate(ctx, []Entity{
		{Name: "multiarch", Repository: "library", Tag: "latest"},
	}))

	dstRef, err := name.ParseReference(dstAddr+"/library/multiarch:latest", name.Insecure)
	require.NoError(t, err)
	dstDesc, err := remote.Get(dstRef)
	require.NoError(t, err)

	require.True(t, dstDesc.MediaType.IsIndex(),
		"expected destination to preserve multi-arch index against Zot, got %s", dstDesc.MediaType)

	// Zot accepted the rebuilt OCI index. Verify all source platform manifests
	// made it through, with rewritten media types.
	dstIdx, err := remote.Index(dstRef)
	require.NoError(t, err)
	dstManifest, err := dstIdx.IndexManifest()
	require.NoError(t, err)
	require.Len(t, dstManifest.Manifests, 3, "expected 3 platform manifests preserved against Zot")
	for _, m := range dstManifest.Manifests {
		require.Equal(t, types.OCIManifestSchema1, m.MediaType,
			"expected child manifests rewritten to OCI for Zot compat, got %s", m.MediaType)
	}
}

// startZotForTest starts an embedded Zot registry on a random port. Mirrors the
// helper in internal/registry/zot_storage_test.go; duplicated here to avoid
// cross-package test-helper export.
func startZotForTest(t *testing.T, storageDir string) string {
	t.Helper()

	conf := config.New()
	conf.Storage.RootDirectory = storageDir
	conf.Storage.GC = false
	conf.Storage.Dedupe = false
	conf.HTTP.Address = "127.0.0.1"
	conf.HTTP.Port = "0"
	conf.Log.Level = "error"

	ctlr := api.NewController(conf)
	require.NoError(t, ctlr.Init())

	go func() {
		if err := ctlr.Run(); err != nil && err != http.ErrServerClosed {
			t.Logf("zot run error: %v", err)
		}
	}()
	t.Cleanup(ctlr.Shutdown)

	require.Eventually(t, func() bool {
		return ctlr.GetPort() != 0
	}, 5*time.Second, 50*time.Millisecond, "zot did not start in time")

	addr := fmt.Sprintf("127.0.0.1:%d", ctlr.GetPort())

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/v2/")
		if err != nil {
			return false
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("error closing response body: %v", err)
			}
		}()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "/v2/ not ready")

	return addr
}
