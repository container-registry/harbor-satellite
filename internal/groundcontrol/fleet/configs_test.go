package fleet_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/testutil/pending"
	"github.com/stretchr/testify/require"
)

func configChanged(id fleet.ConfigID, name string) fleet.Event {
	return fleet.Event{
		Kind:      fleet.EventConfigChanged,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateConfig, ID: int64(id)},
		Payload:   fleet.ChangedPayload{Name: name},
	}
}

func TestConfigService_CreateAppendsConfigChanged(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
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
	require.Equal(t, []fleet.Event{configChanged(3, "edge-config")}, w.repo.Events.Appended())
	insideTx(t, w.rec, "Events.Append")
	noRegistryCalls(t, w.rec)
}

func TestConfigService_UpdateMergePatches(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
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
	require.Equal(t, []fleet.Event{configChanged(3, "edge-config")}, w.repo.Events.Appended())
	noRegistryCalls(t, w.rec)
}

func TestConfigService_DeleteInUseIsErrInUse(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 3, Name: "edge-config"}}
	w.repo.Configs.InUseIDs = map[fleet.ConfigID]bool{3: true}

	err := w.services(t).Configs.Delete(context.Background(), "edge-config")

	require.ErrorIs(t, err, fleet.ErrInUse)
	require.Zero(t, w.rec.Count("Configs.Delete"))
	require.Empty(t, w.repo.Events.Appended())
}

func TestConfigService_DeleteAppendsATombstone(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()
	w.repo.Configs.ByName = map[string]fleet.Config{"edge-config": {ID: 3, Name: "edge-config"}}

	require.NoError(t, w.services(t).Configs.Delete(context.Background(), "edge-config"))

	require.Equal(t, fleet.ConfigID(3), arg[fleet.ConfigID](t, onlyCall(t, w.rec, "Configs.Delete"), 0))
	require.Equal(t, []fleet.Event{{
		Kind:      fleet.EventConfigDeleted,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateConfig, ID: 3},
		Payload:   fleet.ConfigDeletedPayload{Name: "edge-config", StateRef: stateRef("config", "edge-config")},
	}}, w.repo.Events.Appended())
	noRegistryCalls(t, w.rec)
}

func TestConfigService_AssignUnknownIsErrNotFound(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}

	err := w.services(t).Configs.Assign(context.Background(), fleet.AssignConfigCmd{Satellite: "edge-01", Config: "missing"})

	require.ErrorIs(t, err, fleet.ErrNotFound)
	require.Zero(t, w.rec.Count("Satellites.SetConfig"))
	require.Empty(t, w.repo.Events.Appended())
}

func TestConfigService_AssignIsAssignmentChanged(t *testing.T) {
	pending.Plan(t, "2.5", pendingServices)
	w := newWorld()
	w.repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}
	w.repo.Configs.ByName = map[string]fleet.Config{"other": {ID: 4, Name: "other"}}

	require.NoError(t, w.services(t).Configs.Assign(context.Background(), fleet.AssignConfigCmd{Satellite: "edge-01", Config: "other"}))

	set := onlyCall(t, w.rec, "Satellites.SetConfig")
	require.Equal(t, fleet.SatelliteID(7), arg[fleet.SatelliteID](t, set, 0))
	require.Equal(t, fleet.ConfigID(4), arg[fleet.ConfigID](t, set, 1))
	require.Equal(t, []fleet.Event{{
		Kind:      fleet.EventAssignmentChanged,
		Aggregate: fleet.Aggregate{Type: fleet.AggregateSatellite, ID: 7},
		Payload:   fleet.ChangedPayload{Name: "edge-01"},
	}}, w.repo.Events.Appended())
	noRegistryCalls(t, w.rec)
}
