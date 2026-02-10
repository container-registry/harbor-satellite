package registry

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"
	"zotregistry.dev/zot/pkg/api"
	"zotregistry.dev/zot/pkg/api/config"
)

func startZotRegistry(t *testing.T, storageDir string) string {
	t.Helper()

	conf := config.New()
	conf.Storage.RootDirectory = storageDir
	conf.Storage.GC = false
	conf.Storage.Dedupe = false
	conf.HTTP.Address = "127.0.0.1"
	conf.HTTP.Port = "0"
	conf.Log.Level = "error"

	ctlr := api.NewController(conf)

	err := ctlr.Init()
	require.NoError(t, err)

	go func() {
		if err := ctlr.Run(); err != nil && err != http.ErrServerClosed {
			t.Logf("zot run error: %v", err)
		}
	}()
	t.Cleanup(ctlr.Shutdown)

	// Wait for kernel to assign a port
	require.Eventually(t, func() bool {
		return ctlr.GetPort() != 0
	}, 5*time.Second, 50*time.Millisecond, "zot did not start in time")

	addr := fmt.Sprintf("127.0.0.1:%d", ctlr.GetPort())

	// Wait for /v2/ to respond 200
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/v2/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "/v2/ not ready")

	return addr
}

func pushTestImage(t *testing.T, addr, repo, tag string) v1.Image {
	t.Helper()

	img, err := random.Image(1024, 1)
	require.NoError(t, err)

	// Convert to OCI manifest format (Zot rejects Docker v2 manifests)
	img = mutate.MediaType(img, types.OCIManifestSchema1)

	ref, err := name.ParseReference(addr+"/"+repo+":"+tag, name.Insecure)
	require.NoError(t, err)

	require.NoError(t, remote.Write(ref, img))
	return img
}

func pullTestImage(t *testing.T, addr, repo, tag string) v1.Image {
	t.Helper()

	ref, err := name.ParseReference(addr+"/"+repo+":"+tag, name.Insecure)
	require.NoError(t, err)

	img, err := remote.Image(ref)
	require.NoError(t, err)
	return img
}

func TestZotStorageDirectory(t *testing.T) {
	tests := []struct {
		name string
		repo string
		tag  string
	}{
		{"library/alpine", "library/alpine", "latest"},
		{"myproject/app", "myproject/app", "v1.0"},
		{"org/team/service", "org/team/service", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageDir := t.TempDir()
			addr := startZotRegistry(t, storageDir)

			pushed := pushTestImage(t, addr, tt.repo, tt.tag)

			// Verify repo directory was created on disk
			repoDir := filepath.Join(storageDir, tt.repo)
			_, err := os.Stat(repoDir)
			require.NoError(t, err, "repo dir should exist: %s", repoDir)

			// Verify blobs/sha256 exists and has content
			blobDir := filepath.Join(repoDir, "blobs", "sha256")
			entries, err := os.ReadDir(blobDir)
			require.NoError(t, err, "blobs/sha256 dir should exist")
			require.NotEmpty(t, entries, "blobs/sha256 should have files")

			// Pull and verify digest matches
			pulled := pullTestImage(t, addr, tt.repo, tt.tag)

			pushedDigest, err := pushed.Digest()
			require.NoError(t, err)

			pulledDigest, err := pulled.Digest()
			require.NoError(t, err)

			require.Equal(t, pushedDigest, pulledDigest, "pulled digest should match pushed")
		})
	}
}

func TestZotStorageIsolation(t *testing.T) {
	storageA := t.TempDir()
	storageB := t.TempDir()

	addrA := startZotRegistry(t, storageA)
	addrB := startZotRegistry(t, storageB)

	repo := "library/alpine"
	tag := "latest"

	// Push to registry A only
	pushTestImage(t, addrA, repo, tag)

	// Storage A should have the repo dir
	_, err := os.Stat(filepath.Join(storageA, repo))
	require.NoError(t, err, "storage A should have repo dir")

	// Storage B should NOT have the repo dir
	_, err = os.Stat(filepath.Join(storageB, repo))
	require.True(t, os.IsNotExist(err), "storage B should not have repo dir")

	// Pull from A succeeds
	_ = pullTestImage(t, addrA, repo, tag)

	// Pull from B fails
	refB, err := name.ParseReference(addrB+"/"+repo+":"+tag, name.Insecure)
	require.NoError(t, err)

	_, err = remote.Image(refB)
	require.Error(t, err, "pull from registry B should fail")
}
