package app_test

import (
	"database/sql"
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/app"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/dbpool"
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/env"
	"github.com/stretchr/testify/require"
)

// Webhooks and robot rotation both seal with DATA_ENCRYPTION_KEY; a
// missing key names the setting that needs it, not the other one.
func TestMissingDataEncryptionKeyNamesTheFeatureThatNeedsIt(t *testing.T) {
	pools := &dbpool.Pools{API: new(sql.DB)}
	harbor := env.Harbor{URL: "https://harbor.example", RobotDurationDays: "30"}

	_, err := app.NewFleet(app.FleetDeps{Pools: pools, Harbor: harbor, Webhook: env.Webhook{Enabled: true}})
	require.ErrorIs(t, err, fleet.ErrInvalid)
	require.ErrorContains(t, err, "WEBHOOKS_ENABLED")
	require.ErrorContains(t, err, "DATA_ENCRYPTION_KEY")
	require.NotContains(t, err.Error(), "ROBOT_ROTATION_ENABLED")

	_, err = app.NewFleet(app.FleetDeps{Pools: pools, Harbor: harbor, RobotRotation: env.RobotRotation{Enabled: true}})
	require.ErrorIs(t, err, fleet.ErrInvalid)
	require.ErrorContains(t, err, "ROBOT_ROTATION_ENABLED")
	require.ErrorContains(t, err, "DATA_ENCRYPTION_KEY")
	require.NotContains(t, err.Error(), "WEBHOOKS_ENABLED")

	_, err = app.NewSealer("not base64!")
	require.ErrorContains(t, err, "DATA_ENCRYPTION_KEY is not base64")
	_, err = app.NewSealer("c2hvcnQ=")
	require.ErrorContains(t, err, "want 32")
}
