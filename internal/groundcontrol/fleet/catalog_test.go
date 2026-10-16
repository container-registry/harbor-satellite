package fleet_test

import (
	"testing"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/stretchr/testify/require"
)

func TestEventPayloads_CoversEveryKind(t *testing.T) {
	payloads := fleet.EventPayloads()
	require.Len(t, payloads, len(fleet.EventKinds()))
	for _, kind := range fleet.EventKinds() {
		require.Contains(t, payloads, kind)
		require.NotNil(t, payloads[kind], kind)
	}
	for _, kind := range []fleet.EventKind{
		fleet.EventEdgeReplicated, fleet.EventEdgeDeleted, fleet.EventEdgeArtifactFailed,
		fleet.EventEdgePullThrough, fleet.EventEdgeCRIConfigured, fleet.EventEdgeJournalGap,
	} {
		require.Contains(t, payloads, kind, "the six edge kinds")
	}
}
