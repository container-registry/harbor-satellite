package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet/fleettest"
	"github.com/stretchr/testify/require"
)

// fakeFleetServer is a Server whose handlers run on recording fakes
// (plan item 1.5), so a handler test needs neither Postgres nor Harbor.
func fakeFleetServer(t *testing.T) (*Server, *fleettest.Repository) {
	t.Helper()
	rec := fleettest.NewRecorder()
	repo := fleettest.NewRepository(rec)
	svc, err := fleet.NewServices(fleet.Deps{Repository: repo, Registry: fleettest.NewRegistry(rec), Settings: fleet.Settings{RegistryURL: "http://harbor:8080"}})
	require.NoError(t, err)
	return &Server{services: svc}, repo
}

func serve(t *testing.T, h func(w http.ResponseWriter, r *http.Request), method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(method, path, nil))
	return rr
}

func TestConfigHandlers_OnFakes(t *testing.T) {
	s, repo := fakeFleetServer(t)
	repo.Configs.ByName = map[string]fleet.Config{
		"test-config": {ID: 1, Name: "test-config", RegistryURL: "http://harbor:8080", Document: json.RawMessage(`{"app_config":{}}`)},
		"used-config": {ID: 2, Name: "used-config", Document: json.RawMessage(`{}`)},
	}
	repo.Configs.InUseIDs = map[fleet.ConfigID]bool{2: true}

	rr := serve(t, s.ListConfigs, http.MethodGet, "/api/configs")
	require.Equal(t, http.StatusOK, rr.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	require.Len(t, list, 2)
	require.Equal(t, "test-config", list[0]["ConfigName"], "the sqlc row field names stay")

	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.GetConfig(w, r, "test-config") }, http.MethodGet, "/api/configs/test-config")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"RegistryUrl":"http://harbor:8080"`)
	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.GetConfig(w, r, "missing") }, http.MethodGet, "/api/configs/missing")
	require.Equal(t, http.StatusNotFound, rr.Code)

	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.DeleteConfig(w, r, "used-config") }, http.MethodDelete, "/api/configs/used-config")
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "Cannot delete config that is in use")
	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.DeleteConfig(w, r, "missing") }, http.MethodDelete, "/api/configs/missing")
	require.Equal(t, http.StatusInternalServerError, rr.Code)

	repo.FailOn("Configs.List", errors.New("db error"))
	require.Equal(t, http.StatusInternalServerError, serve(t, s.ListConfigs, http.MethodGet, "/api/configs").Code)
}

func TestGroupHandlers_OnFakes(t *testing.T) {
	s, repo := fakeFleetServer(t)
	repo.Groups.ByName = map[string]fleet.Group{"edge": {ID: 1, Name: "edge", Projects: []string{"library"}}}
	repo.Groups.MembersOf = map[fleet.GroupID][]fleet.SatelliteID{1: {7}}
	repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01"}}

	rr := serve(t, s.ListGroups, http.MethodGet, "/api/groups")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"GroupName":"edge"`)
	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.GetGroup(w, r, "missing") }, http.MethodGet, "/api/groups/missing")
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "Group not found")
	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.ListGroupSatellites(w, r, "edge") }, http.MethodGet, "/api/groups/edge/satellites")
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"Name":"edge-01"`)
	rr = serve(t, func(w http.ResponseWriter, r *http.Request) { s.ListGroupSatellites(w, r, "missing") }, http.MethodGet, "/api/groups/missing/satellites")
	require.Equal(t, http.StatusNotFound, rr.Code)

	repo.FailOn("Groups.List", errors.New("db error"))
	require.Equal(t, http.StatusInternalServerError, serve(t, s.ListGroups, http.MethodGet, "/api/groups").Code)
}

func TestSatelliteHandlers_OnFakes(t *testing.T) {
	s, repo := fakeFleetServer(t)
	repo.Satellites.ByName = map[string]fleet.Satellite{"edge-01": {ID: 7, Name: "edge-01", HeartbeatInterval: "@every 00h00m30s"}, "edge-02": {ID: 8, Name: "edge-02"}}

	rr := serve(t, s.ListSatellites, http.MethodGet, "/api/satellites")
	require.Equal(t, http.StatusOK, rr.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
	require.Len(t, list, 2)
	require.Equal(t, map[string]any{"String": "@every 00h00m30s", "Valid": true}, list[0]["HeartbeatInterval"])
	lastSeen, ok := list[1]["LastSeen"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, lastSeen["Valid"])

	repo.FailOn("Satellites.List", errors.New("db error"))
	require.Equal(t, http.StatusInternalServerError, serve(t, s.ListSatellites, http.MethodGet, "/api/satellites").Code)
}
