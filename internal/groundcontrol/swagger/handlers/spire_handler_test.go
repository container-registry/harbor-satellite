package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/server/operations/spire"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/stretchr/testify/require"
)

func TestSPIREStatusResponseKeepsFalseFlags(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(swaggermodels.SPIREStatusResponse{})
	require.NoError(t, err)
	require.JSONEq(t, `{"connected":false,"enabled":false}`, string(payload))
}

func TestAgentListResponseJSON(t *testing.T) {
	t.Parallel()

	response := swaggermodels.AgentListResponse{
		Agents: []swaggermodels.AgentInfoResponse{
			{SpiffeID: "spiffe://example.test/agent/edge-01", AttestationType: "x509pop"},
			{SpiffeID: "spiffe://example.test/agent/edge-02", AttestationType: "sshpop"},
		},
	}
	payload, err := json.Marshal(response)
	require.NoError(t, err)

	var decoded swaggermodels.AgentListResponse
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Len(t, decoded.Agents, 2)
	require.Equal(t, "x509pop", decoded.Agents[0].AttestationType)
	require.Equal(t, "sshpop", decoded.Agents[1].AttestationType)
}

func TestSPIREHandlersReportNotImplemented(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		responder middleware.Responder
	}{
		{name: "status", responder: GetSpireStatus(spire.GetSpireStatusParams{}, handlerTestPrincipal)},
		{name: "agents", responder: ListSpireAgents(spire.ListSpireAgentsParams{}, handlerTestPrincipal)},
		{name: "registration", responder: RegisterSatelliteWithSpiffe(spire.RegisterSatelliteWithSpiffeParams{}, handlerTestPrincipal)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			test.responder.WriteResponse(recorder, runtime.JSONProducer())
			require.Equal(t, http.StatusNotImplemented, recorder.Code)
		})
	}
}
