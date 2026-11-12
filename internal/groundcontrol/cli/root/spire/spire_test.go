package spire_test

import (
	"net/http"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/cli/clitest"
	"github.com/stretchr/testify/require"
)

func TestSpireCommandsCallTheirRoutes(t *testing.T) {
	clitest.CheckRoutes(t, []clitest.Route{
		{Name: "agents", Args: []string{"get", "spire-agents"}, Answer: `{"agents":[]}`, Want: clitest.Request{Method: http.MethodGet, Path: "/api/spire/agents"}},
		{
			Name: "agents by attestation type", Args: []string{"get", "spire-agents", "--attestation-type", "x509pop"}, Answer: `{"agents":[]}`,
			Want: clitest.Request{Method: http.MethodGet, Path: "/api/spire/agents", Query: "attestation_type=x509pop"},
		},
		{Name: "status", Args: []string{"get", "spire-status"}, Answer: `{"healthy":true}`, Want: clitest.Request{Method: http.MethodGet, Path: "/api/spire/status"}},
	})
}

func TestSpireCommandShowsServerError(t *testing.T) {
	server := clitest.NewServer(t, http.StatusServiceUnavailable, `{"code":503,"message":"SPIRE server not configured"}`)
	_, err := clitest.RunAuthenticated(t, server, nil, "get", "spire-status")
	require.ErrorContains(t, err, "SPIRE server not configured")
	_, err = clitest.RunAuthenticated(t, server, nil, "get", "spire-agents")
	require.ErrorContains(t, err, "503")
}
