package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type specResponses struct {
	Paths map[string]map[string]struct {
		Responses map[string]any `json:"responses"`
	} `json:"paths"`
}

// Both registration routes answer 403 for a revoked satellite
// (TestRevoke_SyncAndZTRReturn403UntilUnrevoked); the spec must say so,
// or a generated client has no type for the answer.
func TestSpec_RegistrationDocumentsRevoked403(t *testing.T) {
	rec := httptest.NewRecorder()
	revoked := fmt.Errorf("satellite edge-01: %w", fleet.ErrRevoked)
	handleFleetError(rec, revoked, ztrFailure(revoked))
	require.Equal(t, http.StatusForbidden, rec.Code)

	data, err := os.ReadFile("../../../spec/groundcontrol/openapi.yaml")
	require.NoError(t, err)
	var spec specResponses
	require.NoError(t, yaml.Unmarshal(data, &spec))
	for path, method := range map[string]string{"/satellites/ztr": "post", "/satellites/spiffe-ztr": "get"} {
		op, ok := spec.Paths[path][method]
		require.True(t, ok, "%s %s", method, path)
		require.Contains(t, op.Responses, "403", "%s %s", method, path)
	}
}
