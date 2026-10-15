package state

import (
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestExecute_ImageStaysWhileAnotherGroupWantsIt(t *testing.T) {
	pending.Plan(t, "D12", "each group deletes what it dropped although another group in the same store still wants it")

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
	process := NewFetchAndReplicateStateProcess(cm, "", root, &nop)
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
