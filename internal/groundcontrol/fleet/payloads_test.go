package fleet_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

// The payloads become the public event catalogue (plan item 9); this
// freezes their JSON.
func TestEventPayloads_JSON(t *testing.T) {
	ref := fleet.ArtifactRef{Registry: "http://harbor.example", Repository: "satellite/satellite-state/edge-01/state", Tag: "latest"}
	for name, tc := range map[string]struct {
		payload any
		want    string
	}{
		"changed": {fleet.ChangedPayload{Name: "edge-01"}, `{"name":"edge-01"}`},
		"satellite lifecycle": {
			fleet.SatelliteLifecyclePayload{Name: "edge-01", State: fleet.StateStale, PreviousState: fleet.StateOnline, LastSeen: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)},
			`{"name":"edge-01","state":"stale","previous_state":"online","last_seen":"2026-10-15T12:00:00Z"}`,
		},
		"revoked before any beat": {
			fleet.SatelliteLifecyclePayload{Name: "edge-01", State: fleet.StateRevoked, PreviousState: fleet.StatePending},
			`{"name":"edge-01","state":"revoked","previous_state":"pending"}`,
		},
		"satellite deleted": {
			fleet.SatelliteDeletedPayload{Name: "edge-01", RobotHarborIDs: []int64{42, 43}, SPIFFEID: "spiffe://harbor-satellite.local/satellite/region/default/edge-01", StateRef: ref},
			`{"name":"edge-01","robot_harbor_ids":[42,43],"spiffe_id":"spiffe://harbor-satellite.local/satellite/region/default/edge-01","state_ref":{"registry":"http://harbor.example","repository":"satellite/satellite-state/edge-01/state","tag":"latest"}}`,
		},
		"token satellite deleted": {
			fleet.SatelliteDeletedPayload{Name: "edge-01", RobotHarborIDs: []int64{42}, StateRef: ref},
			`{"name":"edge-01","robot_harbor_ids":[42],"state_ref":{"registry":"http://harbor.example","repository":"satellite/satellite-state/edge-01/state","tag":"latest"}}`,
		},
		"group deleted": {
			fleet.GroupDeletedPayload{Name: "g1", Members: []fleet.SatelliteID{7, 8}, StateRef: ref},
			`{"name":"g1","members":[7,8],"state_ref":{"registry":"http://harbor.example","repository":"satellite/satellite-state/edge-01/state","tag":"latest"}}`,
		},
		"config deleted": {
			fleet.ConfigDeletedPayload{Name: "edge-config", StateRef: ref},
			`{"name":"edge-config","state_ref":{"registry":"http://harbor.example","repository":"satellite/satellite-state/edge-01/state","tag":"latest"}}`,
		},
	} {
		got, err := json.Marshal(tc.payload)
		require.NoError(t, err, name)
		require.Equal(t, tc.want, string(got), name)
	}
}
