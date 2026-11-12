package state

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/shared/inventory"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/container-registry/harbor-satellite/pkg/config"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/stretchr/testify/require"
)

// byoReportingCM is a BYO satellite whose local registry holds one image.
func byoReportingCM(t *testing.T, gcURL string) *config.ConfigManager {
	t.Helper()
	reg := httptest.NewServer(registry.New())
	t.Cleanup(reg.Close)
	img, err := random.Image(256, 1)
	require.NoError(t, err)
	require.NoError(t, crane.Push(img, reg.Listener.Addr().String()+"/library/alpine:3.20", crane.Insecure))

	dir := t.TempDir()
	cfg := &config.Config{
		StateConfig: config.StateConfig{StateURL: "http://registry/satellite/satellite-state/test-sat/state:latest"},
		AppConfig: config.AppConfig{
			GroundControlURL:         config.URL(gcURL),
			HeartbeatInterval:        "@every 30s",
			UseUnsecure:              true,
			BringOwnRegistry:         true,
			LocalRegistryCredentials: config.RegistryCredentials{URL: config.URL(reg.URL)},
		},
	}
	cm, err := config.NewConfigManager(filepath.Join(dir, "config.json"), filepath.Join(dir, "prev.json"), "token", gcURL, false, cfg)
	require.NoError(t, err)
	return cm
}

func TestStatusReport_SendsInventoryOnlyWhenChangedOrRequested(t *testing.T) {
	pending.Plan(t, "4.4", "every heartbeat sends the full cached image list")
	var (
		mu        sync.Mutex
		received  []map[string]json.RawMessage
		requested bool
	)
	gc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		received = append(received, body)
		resp := map[string]any{"events": []string{}, "inventory_requested": requested}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(gc.Close)
	p := &StatusReportingProcess{name: "test", mu: &sync.Mutex{}, cm: byoReportingCM(t, gc.URL)}
	beat := func() map[string]json.RawMessage {
		t.Helper()
		require.NoError(t, p.Execute(testContext()))
		mu.Lock()
		defer mu.Unlock()
		return received[len(received)-1]
	}

	first := beat()
	require.Contains(t, first, "cached_images", "the first report carries the inventory")
	require.Contains(t, first, "inventory_digest")
	var digest string
	require.NoError(t, json.Unmarshal(first["inventory_digest"], &digest))
	var images []CachedImage
	require.NoError(t, json.Unmarshal(first["cached_images"], &images))
	items := make([]inventory.Item, 0, len(images))
	for _, img := range images {
		items = append(items, inventory.Item{Reference: img.Reference, SizeBytes: img.SizeBytes})
	}
	require.Equal(t, inventory.Digest(items), digest)
	require.Equal(t, digest, InventoryDigest(images))

	second := beat()
	require.NotContains(t, second, "cached_images", "an unchanged inventory sends only its digest")
	require.JSONEq(t, string(first["inventory_digest"]), string(second["inventory_digest"]))

	mu.Lock()
	requested = true
	mu.Unlock()
	beat()
	require.Contains(t, beat(), "cached_images", "Ground Control asked for the inventory")
}
