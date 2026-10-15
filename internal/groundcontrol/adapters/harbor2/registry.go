// Package harbor2 implements fleet.Registry over the Harbor v2 API and
// crane (plan item 1.3). Each Registry builds its own client from Config:
// no process-wide client, no env reads. It takes over from
// internal/groundcontrol/harbor, utils/helper.go and the crane pushes in
// server/helpers.go, whose global client stays until the handlers move to
// the fleet services.
package harbor2

import (
	"context"
	"net/http"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

var _ fleet.Registry = (*Registry)(nil)

type Config struct {
	URL, Username, Password string
	// Insecure talks plain HTTP to the registry, as an http:// URL does
	// today.
	Insecure bool
	// HTTPClient is used for the API and the registry; nil is a default
	// client.
	HTTPClient *http.Client
}

type Registry struct{}

func New(cfg Config) (*Registry, error) {
	return nil, notimpl.Err("1.3 harbor2 registry")
}

func (r *Registry) FindRobot(ctx context.Context, name string) (fleet.RobotCredentials, error) {
	return fleet.RobotCredentials{}, notimpl.Err("1.3 harbor2 find robot")
}

// CreateRobot creates a system robot that may read and pull the spec's
// projects, as harbor.RobotAccountTemplate does, for spec.Duration rounded
// up to whole days because Harbor counts in days.
func (r *Registry) CreateRobot(ctx context.Context, spec fleet.RobotSpec) (fleet.RobotCredentials, error) {
	return fleet.RobotCredentials{}, notimpl.Err("1.3 harbor2 create robot")
}

// SetRobotProjects reads the robot and writes it back with the new
// permissions, because Harbor's update replaces the whole robot.
func (r *Registry) SetRobotProjects(ctx context.Context, harborID int64, projects []string) error {
	return notimpl.Err("1.3 harbor2 set robot projects")
}

func (r *Registry) RotateRobotSecret(ctx context.Context, harborID int64) (fleet.RobotCredentials, error) {
	return fleet.RobotCredentials{}, notimpl.Err("1.3 harbor2 rotate robot secret")
}

func (r *Registry) DeleteRobot(ctx context.Context, harborID int64) error {
	return notimpl.Err("1.3 harbor2 delete robot")
}

func (r *Registry) PushState(ctx context.Context, ref fleet.ArtifactRef, payload []byte) (fleet.Digest, error) {
	return "", notimpl.Err("1.3 harbor2 push state")
}

// DeleteState deletes the state repository through the Harbor API, which
// expects the repository name URL-encoded twice.
func (r *Registry) DeleteState(ctx context.Context, ref fleet.ArtifactRef) error {
	return notimpl.Err("1.3 harbor2 delete state")
}

func (r *Registry) EnsureProject(ctx context.Context, project string) error {
	return notimpl.Err("1.3 harbor2 ensure project")
}

func (r *Registry) ReplicationPolicyExists(ctx context.Context, name string) (bool, error) {
	return false, notimpl.Err("1.3 harbor2 replication policy exists")
}

func (r *Registry) Ping(ctx context.Context) error {
	return notimpl.Err("1.3 harbor2 ping")
}
