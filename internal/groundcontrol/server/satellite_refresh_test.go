package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefreshRejectsExpiredRobot(t *testing.T) {
	h := newGCHarness(t)
	h.createConfig("edge-config")
	h.syncGroup("edge-group", "library/alpine")
	creds := h.onboard("edge-01", "edge-config", "edge-group")
	_, err := h.db.ExecContext(context.Background(),
		`UPDATE robot_accounts SET robot_expiry = now() - interval '1 minute' WHERE robot_name = $1`, creds.Username)
	require.NoError(t, err)

	h.mustDo(http.MethodPost, "/satellites/refresh", nil, basicAuth(creds.Username, creds.Password), http.StatusUnauthorized)
}

func TestRefreshRequiresSatelliteCredentials(t *testing.T) {
	h := newGCHarness(t)

	h.mustDo(http.MethodPost, "/satellites/refresh", nil, nil, http.StatusUnauthorized)
	h.mustDo(http.MethodPost, "/satellites/refresh", nil, adminBasicAuth, http.StatusUnauthorized)
}
