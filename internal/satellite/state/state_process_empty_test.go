package state

import (
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/satellite/store"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Ground Control publishes a satellite state without groups once a
// satellite has none (D10); the satellite must stop replicating the groups
// it had instead of failing.
func TestExecute_SatelliteStateWithoutGroups(t *testing.T) {
	ctx := testContext()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	manifest, _ := pushRetagTestImage(t, host, "team/app", "v1", []byte("layer of the only image"))

	groupStateRef := host + "/satellite/group-state/edge/state:latest"
	configStateRef := host + "/satellite/config-state/edge/state:latest"
	satelliteStateRef := host + "/satellite/satellite-state/sat/state:latest"
	pushStateArtifact(t, configStateRef, config.Config{AppConfig: config.AppConfig{UseUnsecure: true}})
	pushStateArtifact(t, groupStateRef, State{Registry: server.URL, Artifacts: []Artifact{
		{Repository: "team/app", Tags: []string{"latest"}, Digest: manifest.Digest.String(), Type: "image"},
	}})
	pushStateArtifact(t, satelliteStateRef, SatelliteState{States: []string{"http://" + groupStateRef}, Config: "http://" + configStateRef})

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
	require.NoError(t, process.Execute(ctx))
	require.Len(t, process.stateMap, 1)

	// What GC pushes for a satellite without groups: the generated type
	// omits the empty list.
	pushStateArtifact(t, satelliteStateRef, map[string]string{"config": "http://" + configStateRef})

	require.NoError(t, process.Execute(ctx))
	require.Empty(t, process.stateMap, "the satellite still tracks the group it left")
}
