package fleet_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func TestConfigService_CreatePushesState(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Configs.NextID = 3
	doc := json.RawMessage(`{"app_config":{"log_level":"info"}}`)

	cfg, err := w.services(t).Configs.Create(context.Background(), fleet.CreateConfigCmd{Name: "edge-config", Document: doc})

	require.NoError(t, err)
	require.Equal(t, fleet.ConfigID(3), cfg.ID)
	created := arg[fleet.Config](t, onlyCall(t, w.rec, "Configs.Create"), 0)
	require.Equal(t, "edge-config", created.Name)
	require.Equal(t, registryURL, created.RegistryURL)
	require.JSONEq(t, string(doc), string(created.Document))
	push := onlyCall(t, w.rec, "PushState")
	require.Equal(t, stateRef("config", "edge-config"), arg[fleet.ArtifactRef](t, push, 0))
	require.JSONEq(t, string(doc), string(arg[[]byte](t, push, 1)))
	require.Equal(t, 1, w.rec.Count("Commit"))
}

func TestConfigService_UpdateMergePatches(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {
		ID:       3,
		Name:     "edge-config",
		Document: json.RawMessage(`{"app_config":{"log_level":"info","heartbeat_interval":"@every 00h00m30s"}}`),
	}}
	patch := json.RawMessage(`{"app_config":{"log_level":"debug","heartbeat_interval":null}}`)
	want := `{"app_config":{"log_level":"debug"}}`

	cfg, err := w.services(t).Configs.Update(context.Background(), fleet.UpdateConfigCmd{Name: "edge-config", Patch: patch})

	require.NoError(t, err)
	require.JSONEq(t, want, string(cfg.Document))
	updated := arg[fleet.Config](t, onlyCall(t, w.rec, "Configs.Update"), 0)
	require.Equal(t, fleet.ConfigID(3), updated.ID)
	require.JSONEq(t, want, string(updated.Document))
	push := onlyCall(t, w.rec, "PushState")
	require.Equal(t, stateRef("config", "edge-config"), arg[fleet.ArtifactRef](t, push, 0))
	require.JSONEq(t, want, string(arg[[]byte](t, push, 1)))
}

func TestConfigService_DeleteInUseIsErrInUse(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 3, Name: "edge-config"}}
	w.repo.Configs.InUseIDs = map[fleet.ConfigID]bool{3: true}

	err := w.services(t).Configs.Delete(context.Background(), "edge-config")

	require.ErrorIs(t, err, fleet.ErrInUse)
	require.Zero(t, w.rec.Count("Configs.Delete"))
	require.Zero(t, w.rec.Count("DeleteState"))
}

func TestConfigService_DeleteRemovesState(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 3, Name: "edge-config"}}

	require.NoError(t, w.services(t).Configs.Delete(context.Background(), "edge-config"))

	require.Equal(t, fleet.ConfigID(3), arg[fleet.ConfigID](t, onlyCall(t, w.rec, "Configs.Delete"), 0))
	require.Equal(t, stateRef("config", "edge-config"), arg[fleet.ArtifactRef](t, onlyCall(t, w.rec, "DeleteState"), 0))
}

func TestConfigService_AssignUnknownIsErrNotFound(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}

	err := w.services(t).Configs.Assign(context.Background(), fleet.AssignConfigCmd{Satellite: "edge-01", Config: "missing"})

	require.ErrorIs(t, err, fleet.ErrNotFound)
	require.Zero(t, w.rec.Count("Satellites.SetConfig"))
}

func TestConfigService_AssignRepublishesSatelliteState(t *testing.T) {
	pending.Plan(t, "1.2", pendingServices)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Configs.ByName = map[string]fleet.Config{"other": {ID: 4, Name: "other"}}
	w.repo.Satellites.ConfigOf = map[fleet.SatelliteID]fleet.Config{7: {ID: 4, Name: "other"}}

	require.NoError(t, w.services(t).Configs.Assign(context.Background(), fleet.AssignConfigCmd{Satellite: "edge-01", Config: "other"}))

	set := onlyCall(t, w.rec, "Satellites.SetConfig")
	require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, set, 0))
	require.Equal(t, fleet.ConfigID(4), arg[fleet.ConfigID](t, set, 1))
	_, config := pushedSatelliteState(t, onlyCall(t, w.rec, "PushState"))
	require.Equal(t, stateRef("config", "other").String(), config)
}
