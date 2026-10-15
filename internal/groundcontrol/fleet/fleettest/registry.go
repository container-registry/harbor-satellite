package fleettest

import (
	"context"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.Registry = (*Registry)(nil)

// Registry is a recording fleet.Registry.
type Registry struct {
	*Recorder
	// Robots answers FindRobot by robot name.
	Robots map[string]fleet.RobotCredentials
	// Credentials is what CreateRobot and RotateRobotSecret return.
	Credentials fleet.RobotCredentials
	// Policies answers ReplicationPolicyExists by policy name.
	Policies map[string]bool
	// Digest is what PushState returns.
	Digest fleet.Digest
}

// NewRegistry records into rec, or into a new Recorder when rec is nil.
func NewRegistry(rec *Recorder) *Registry {
	return &Registry{Recorder: orNew(rec)}
}

func (r *Registry) FindRobot(_ context.Context, name string) (fleet.RobotCredentials, error) {
	if err := r.Record("FindRobot", name); err != nil {
		return fleet.RobotCredentials{}, err
	}
	robot, ok := r.Robots[name]
	if !ok {
		return fleet.RobotCredentials{}, fleet.ErrNotFound
	}
	return robot, nil
}

func (r *Registry) CreateRobot(_ context.Context, spec fleet.RobotSpec) (fleet.RobotCredentials, error) {
	if err := r.Record("CreateRobot", spec); err != nil {
		return fleet.RobotCredentials{}, err
	}
	return r.Credentials, nil
}

func (r *Registry) SetRobotAccess(_ context.Context, harborID int64, projects []string, disabled bool) error {
	return r.Record("SetRobotAccess", harborID, projects, disabled)
}

func (r *Registry) RotateRobotSecret(_ context.Context, harborID int64) (fleet.RobotCredentials, error) {
	if err := r.Record("RotateRobotSecret", harborID); err != nil {
		return fleet.RobotCredentials{}, err
	}
	return r.Credentials, nil
}

func (r *Registry) DeleteRobot(_ context.Context, harborID int64) error {
	return r.Record("DeleteRobot", harborID)
}

func (r *Registry) PushState(_ context.Context, ref fleet.ArtifactRef, payload []byte) (fleet.Digest, error) {
	if err := r.Record("PushState", ref, payload); err != nil {
		return "", err
	}
	return r.Digest, nil
}

func (r *Registry) DeleteState(_ context.Context, ref fleet.ArtifactRef) error {
	return r.Record("DeleteState", ref)
}

func (r *Registry) EnsureProject(_ context.Context, project string) error {
	return r.Record("EnsureProject", project)
}

func (r *Registry) ReplicationPolicyExists(_ context.Context, name string) (bool, error) {
	if err := r.Record("ReplicationPolicyExists", name); err != nil {
		return false, err
	}
	return r.Policies[name], nil
}

func (r *Registry) Ping(context.Context) error {
	return r.Record("Ping")
}
