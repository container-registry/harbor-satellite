package fleet

import "github.com/container-registry/harbor-satellite/internal/shared/edgejournal"

// The edge payloads are the record's edgejournal payload with its
// EdgeOrigin at the same level; encoding/json flattens the embedding.
type (
	EdgeReplicationPayload struct {
		edgejournal.Replication
		EdgeOrigin
	}
	EdgePullThroughPayload struct {
		edgejournal.PullThrough
		EdgeOrigin
	}
	EdgeCRIPayload struct {
		edgejournal.CRI
		EdgeOrigin
	}
)

// EventPayloads maps every kind to a zero value of its payload, the public
// event catalogue of plan item 9.2 (spec/events/v1).
func EventPayloads() map[EventKind]any {
	return map[EventKind]any{
		EventSatelliteCreated:   ChangedPayload{},
		EventSatelliteDeleted:   SatelliteDeletedPayload{},
		EventGroupChanged:       ChangedPayload{},
		EventGroupDeleted:       GroupDeletedPayload{},
		EventConfigChanged:      ChangedPayload{},
		EventConfigDeleted:      ConfigDeletedPayload{},
		EventAssignmentChanged:  ChangedPayload{},
		EventSatelliteChanged:   ChangedPayload{},
		EventEdgeReplicated:     EdgeReplicationPayload{},
		EventEdgeDeleted:        EdgeReplicationPayload{},
		EventEdgeArtifactFailed: EdgeReplicationPayload{},
		EventEdgePullThrough:    EdgePullThroughPayload{},
		EventEdgeCRIConfigured:  EdgeCRIPayload{},
		EventEdgeJournalGap:     EdgeJournalGapPayload{},
		EventSatelliteStale:     SatelliteLifecyclePayload{},
		EventSatelliteRevoked:   SatelliteLifecyclePayload{},
		EventSatelliteUnrevoked: SatelliteLifecyclePayload{},
		EventPolicyChanged:      PolicyChangedPayload{},
		EventRobotRotated:       RobotRotatedPayload{},
	}
}
