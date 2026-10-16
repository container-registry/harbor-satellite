package async

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var (
	_ RoutedHandler = (*SatelliteReconciler)(nil)
	_ RoutedHandler = (*GroupReconciler)(nil)
	_ RoutedHandler = (*ConfigReconciler)(nil)
	_ RoutedHandler = (*TombstoneHandler)(nil)
	_ Handler       = (*Router)(nil)
)

// Every reconciler renders from the database whatever the event says,
// publishes only when the rendered digest differs from the published one,
// and returns nil when its aggregate no longer exists.

// SatelliteReconciler publishes a satellite's desired state
// (fleet.RenderSatelliteDesiredState): its robot may pull
// fleet.RobotProjects of its groups (this supersedes D5's helper), and its
// state artifact lists its groups and config, groupless included (D10).
// It renders with robotDisabled = (the view state is revoked), applies
// SetRobotAccess(projects, disabled), then PushState, so a revoked
// satellite's robot is disabled with its projects kept (plan item 4.3).
// For RobotRotated, Handle calls desired.ClearPublished first, so the
// promoted successor gets the projects; the desired document does not
// change, so the fleet is not republished (plan item 8.4).
// It creates the robot when the satellite has none, named
// fleet.RobotName; when the registry answers ErrAlreadyExists, an earlier
// attempt created it before it could be stored, so the robot of exactly
// that name is adopted.
type SatelliteReconciler struct{}

func NewSatelliteReconciler(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*SatelliteReconciler, error) {
	return nil, notimpl.Err("2.3 satellite reconciler")
}

func (r *SatelliteReconciler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{
		fleet.EventSatelliteCreated, fleet.EventAssignmentChanged, fleet.EventSatelliteChanged,
		fleet.EventSatelliteRevoked, fleet.EventSatelliteUnrevoked, fleet.EventRobotRotated,
	}
}

func (r *SatelliteReconciler) Reconcile(ctx context.Context, id fleet.SatelliteID) error {
	return notimpl.Err("2.3 reconcile satellite")
}

func (r *SatelliteReconciler) Handle(ctx context.Context, event OutboxEvent) error {
	return notimpl.Err("2.3 satellite reconciler handle")
}

// GroupReconciler publishes a group's state artifact from group_artifacts,
// then appends one EventSatelliteChanged per member with
// EventSink.AppendToGroupMembers, so a large group fans out in one
// statement and each member is reconciled, retried and locked on its own.
type GroupReconciler struct{}

func NewGroupReconciler(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*GroupReconciler, error) {
	return nil, notimpl.Err("2.3 group reconciler")
}

func (r *GroupReconciler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{fleet.EventGroupChanged}
}

func (r *GroupReconciler) Reconcile(ctx context.Context, id fleet.GroupID) error {
	return notimpl.Err("2.3 reconcile group")
}

func (r *GroupReconciler) Handle(ctx context.Context, event OutboxEvent) error {
	return notimpl.Err("2.3 group reconciler handle")
}

// ConfigReconciler publishes a config's state artifact.
type ConfigReconciler struct{}

func NewConfigReconciler(repo fleet.Repository, registry fleet.Registry, desired DesiredStateStore, settings fleet.Settings) (*ConfigReconciler, error) {
	return nil, notimpl.Err("2.3 config reconciler")
}

func (r *ConfigReconciler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{fleet.EventConfigChanged}
}

func (r *ConfigReconciler) Reconcile(ctx context.Context, id fleet.ConfigID) error {
	return notimpl.Err("2.3 reconcile config")
}

func (r *ConfigReconciler) Handle(ctx context.Context, event OutboxEvent) error {
	return notimpl.Err("2.3 config reconciler handle")
}

// TombstoneHandler deletes what a delete event's payload names
// (fleet.SatelliteDeletedPayload, GroupDeletedPayload,
// ConfigDeletedPayload): robots, the SPIRE entries of the SPIFFE ID, the
// state artifact and the desired-state row. State refs and SPIFFE IDs are
// built from names, so when a live aggregate of the same name exists (it
// was deleted and created again) the handler leaves the artifact and the
// SPIRE entries alone and clears that aggregate's published digest
// instead, so it is published again. A deleted group's members get an
// EventSatelliteChanged each. Objects already gone count as deleted.
type TombstoneHandler struct{}

func NewTombstoneHandler(repo fleet.Repository, registry fleet.Registry, identity fleet.WorkloadIdentity, desired DesiredStateStore) (*TombstoneHandler, error) {
	return nil, notimpl.Err("2.3 tombstone handler")
}

func (h *TombstoneHandler) Kinds() []fleet.EventKind {
	return []fleet.EventKind{fleet.EventSatelliteDeleted, fleet.EventGroupDeleted, fleet.EventConfigDeleted}
}

func (h *TombstoneHandler) Handle(ctx context.Context, event OutboxEvent) error {
	return notimpl.Err("2.3 tombstone")
}
