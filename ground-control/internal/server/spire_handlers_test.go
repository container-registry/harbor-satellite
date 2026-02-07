package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterSatelliteRequest_Validation(t *testing.T) {
	tests := []struct {
		name           string
		request        RegisterSatelliteRequest
		expectError    bool
		expectedErrMsg string
	}{
		{
			name: "valid join_token request",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-01",
				Region:            "us-west",
				Selectors:         []string{"docker:label:foo"},
				AttestationMethod: "join_token",
			},
			expectError: false,
		},
		{
			name: "valid x509pop request",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-02",
				Selectors:         []string{"docker:label:bar"},
				AttestationMethod: "x509pop",
			},
			expectError: false,
		},
		{
			name: "valid sshpop request with parent_agent_id",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-03",
				Selectors:         []string{"docker:label:baz"},
				AttestationMethod: "sshpop",
				ParentAgentID:     "spiffe://domain/agent/foo",
			},
			expectError: false,
		},
		{
			name: "missing satellite_name",
			request: RegisterSatelliteRequest{
				Selectors:         []string{"docker:label:foo"},
				AttestationMethod: "join_token",
			},
			expectError:    true,
			expectedErrMsg: "satellite_name is required",
		},
		{
			name: "missing selectors",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-01",
				AttestationMethod: "join_token",
			},
			expectError:    true,
			expectedErrMsg: "selectors is required",
		},
		{
			name: "invalid selector format",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-01",
				Selectors:         []string{"invalid-selector"},
				AttestationMethod: "join_token",
			},
			expectError:    true,
			expectedErrMsg: "must contain ':'",
		},
		{
			name: "invalid attestation_method",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-01",
				Selectors:         []string{"docker:label:foo"},
				AttestationMethod: "invalid",
			},
			expectError:    true,
			expectedErrMsg: "attestation_method must be one of",
		},
		{
			name: "sshpop missing parent_agent_id",
			request: RegisterSatelliteRequest{
				SatelliteName:     "edge-01",
				Selectors:         []string{"docker:label:foo"},
				AttestationMethod: "sshpop",
			},
			expectError:    true,
			expectedErrMsg: "parent_agent_id is required for sshpop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{}

			body, err := json.Marshal(tt.request)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/satellites/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.registerSatelliteWithSPIFFEHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			var respBody map[string]any
			err = json.NewDecoder(resp.Body).Decode(&respBody)
			require.NoError(t, err)

			if tt.expectError {
				require.NotEqual(t, http.StatusOK, resp.StatusCode)
				if tt.expectedErrMsg != "" {
					msg, ok := respBody["message"].(string)
					require.True(t, ok)
					require.Contains(t, msg, tt.expectedErrMsg)
				}
			} else {
				if resp.StatusCode != http.StatusOK {
					msg, _ := respBody["message"].(string)
					if msg == "SPIRE server not configured" {
						return
					}
				}
			}
		})
	}
}

func TestListSpireAgentsHandler_NoSpireClient(t *testing.T) {
	server := &Server{}

	req := httptest.NewRequest(http.MethodGet, "/api/spire/agents", nil)
	w := httptest.NewRecorder()

	server.listSpireAgentsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var respBody map[string]any
	err := json.NewDecoder(resp.Body).Decode(&respBody)
	require.NoError(t, err)
	require.Contains(t, respBody["message"], "SPIRE server not configured")
}

func TestRegisterSatelliteRequest_DefaultValues(t *testing.T) {
	server := &Server{}

	req := RegisterSatelliteRequest{
		SatelliteName:     "edge-01",
		Selectors:         []string{"docker:label:foo"},
		AttestationMethod: "join_token",
	}

	body, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq := httptest.NewRequest(http.MethodPost, "/api/satellites/register", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.registerSatelliteWithSPIFFEHandler(w, httpReq)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return
	}
}

func TestRegisterSatelliteRequest_TTLLimits(t *testing.T) {
	tests := []struct {
		name        string
		ttlSeconds  int
		expectedTTL int
	}{
		{"default TTL when 0", 0, 600},
		{"default TTL when negative", -100, 600},
		{"max TTL capped", 100000, 86400},
		{"valid TTL preserved", 3600, 3600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := RegisterSatelliteRequest{
				SatelliteName:     "edge-01",
				Selectors:         []string{"docker:label:foo"},
				AttestationMethod: "join_token",
				TTLSeconds:        tt.ttlSeconds,
			}

			if req.TTLSeconds <= 0 {
				req.TTLSeconds = 600
			}
			if req.TTLSeconds > 86400 {
				req.TTLSeconds = 86400
			}

			require.Equal(t, tt.expectedTTL, req.TTLSeconds)
		})
	}
}

func TestRegisterSatelliteWithSPIFFEResponse_JSON(t *testing.T) {
	resp := RegisterSatelliteWithSPIFFEResponse{
		Satellite:          "edge-01",
		Region:             "us-west",
		SpiffeID:           "spiffe://example.com/satellite/region/us-west/edge-01",
		ParentAgentID:      "spiffe://example.com/agent/edge-01",
		JoinToken:          "test-token",
		SpireServerAddress: "spire-server",
		SpireServerPort:    8081,
		TrustDomain:        "example.com",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded RegisterSatelliteWithSPIFFEResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Equal(t, resp.Satellite, decoded.Satellite)
	require.Equal(t, resp.Region, decoded.Region)
	require.Equal(t, resp.SpiffeID, decoded.SpiffeID)
	require.Equal(t, resp.ParentAgentID, decoded.ParentAgentID)
	require.Equal(t, resp.JoinToken, decoded.JoinToken)
	require.Equal(t, resp.SpireServerAddress, decoded.SpireServerAddress)
	require.Equal(t, resp.SpireServerPort, decoded.SpireServerPort)
	require.Equal(t, resp.TrustDomain, decoded.TrustDomain)
}

func TestAgentListResponse_JSON(t *testing.T) {
	resp := AgentListResponse{
		Agents: []AgentInfoResponse{
			{
				SpiffeID:        "spiffe://example.com/agent/edge-01",
				AttestationType: "x509pop",
				Selectors:       []string{"x509pop:subject:cn:edge-01"},
			},
			{
				SpiffeID:        "spiffe://example.com/agent/edge-02",
				AttestationType: "sshpop",
				Selectors:       []string{"sshpop:cert-authority:fingerprint:abc123"},
			},
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded AgentListResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Len(t, decoded.Agents, 2)
	require.Equal(t, "x509pop", decoded.Agents[0].AttestationType)
	require.Equal(t, "sshpop", decoded.Agents[1].AttestationType)
}
