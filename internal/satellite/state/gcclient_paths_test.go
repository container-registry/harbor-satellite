package state

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestZtrProcess_TrailingSlashGroundControlURL(t *testing.T) {
	pending.Plan(t, "D1", "a trailing slash on ground_control_url sends the ZTR to //satellites/ztr")

	var (
		mu   sync.Mutex
		seen []string
	)
	gc := http.NewServeMux()
	gc.HandleFunc("POST /satellites/ztr", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, config.StateConfig{
			StateURL: "http://harbor.test/satellite/satellite-state/edge-01/state:latest",
			RegistryCredentials: config.RegistryCredentials{
				URL: "http://harbor.test", Username: "robot$edge-01", Password: "secret",
			},
		})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		gc.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	gcURL := srv.URL + "/"
	cm, err := config.NewConfigManager(
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "prev.json"),
		"ztr-token", gcURL, false,
		&config.Config{AppConfig: config.AppConfig{GroundControlURL: config.URL(gcURL), UseUnsecure: true}},
	)
	require.NoError(t, err)

	require.NoError(t, NewZtrProcess(cm).Execute(testContext()))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"POST /satellites/ztr"}, seen)
}
