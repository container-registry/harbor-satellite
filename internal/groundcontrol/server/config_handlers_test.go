package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateConfigHandler_InvalidBody(t *testing.T) {
	server, _ := newMockServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/configs", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.CreateConfig(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCreateConfigHandler_EmptyName(t *testing.T) {
	server, _ := newMockServer(t)

	body := mustMarshalJSON(t, map[string]string{"config_name": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/configs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.CreateConfig(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestConfigValueKeepsPeerDistribution(t *testing.T) {
	const withPeers = `{
		"app_config": {
			"peer_distribution": {
				"enabled": true,
				"gc_peers": [{"id": "satellite-b", "url": "https://satellite-b.example:5000"}]
			}
		}
	}`

	var created ConfigValue
	require.NoError(t, json.Unmarshal([]byte(withPeers), &created))
	stored, err := json.Marshal(created)
	require.NoError(t, err)
	require.Contains(t, string(stored), `"gc_peers"`)
	require.Contains(t, string(stored), "satellite-b")

	const optOut = `{"app_config":{"peer_distribution":{"enabled":false}}}`
	var disabled ConfigValue
	require.NoError(t, json.Unmarshal([]byte(optOut), &disabled))
	encoded, err := json.Marshal(disabled)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"peer_distribution"`)
}

func TestConfigValueKeepsNetwork(t *testing.T) {
	const withNetwork = `{
		"app_config": {
			"network": {
				"speed": {"enabled": false, "limit": "10Mbps"},
				"quota": {"enabled": true, "limit": "5GB", "window": "day"}
			}
		}
	}`

	var created ConfigValue
	require.NoError(t, json.Unmarshal([]byte(withNetwork), &created))
	require.Equal(t, "10Mbps", created.AppConfig.Network.Speed.Limit)
	require.Equal(t, "5GB", created.AppConfig.Network.Quota.Limit)
	require.Equal(t, "day", created.AppConfig.Network.Quota.Window)

	stored, err := json.Marshal(created)
	require.NoError(t, err)
	require.Contains(t, string(stored), `"10Mbps"`)
	require.Contains(t, string(stored), `"5GB"`)
	require.Contains(t, string(stored), `"day"`)
}

func TestConfigMergePatchKeepsPeerDistribution(t *testing.T) {
	const patchBody = `{"app_config":{"peer_distribution":{"enabled":true,"gc_peers":[{"id":"satellite-b","url":"https://satellite-b.example:5000"}]}}}`

	var patch ConfigMergePatch
	require.NoError(t, json.Unmarshal([]byte(patchBody), &patch))
	encoded, err := json.Marshal(patch)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"gc_peers"`)
	require.Contains(t, string(encoded), "satellite-b")
}

func TestConfigMergePatchPreservesExplicitNull(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "omitted", input: `{}`},
		{name: "explicit null", input: `{"app_config":null}`},
		{name: "object", input: `{"app_config":{"log_level":"debug"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patch ConfigMergePatch
			require.NoError(t, json.Unmarshal([]byte(tt.input), &patch))

			encoded, err := json.Marshal(patch)
			require.NoError(t, err)
			require.JSONEq(t, tt.input, string(encoded))
		})
	}
}
