package fleet

import (
	"strconv"
	"time"

	"github.com/container-registry/harbor-satellite/internal/shared/notimpl"
)

// DefaultTokenTTL is how long a registration token stays valid.
const DefaultTokenTTL = 24 * time.Hour

// Settings are the deployment values the services need. They replace the
// env.GC reads inside packages (harbor/client.go, server/helpers.go,
// utils/helper.go). There is deliberately no "skip registry" setting:
// plan item 2.6 removes the SKIP_HARBOR_HEALTH_CHECK placeholder secret.
type Settings struct {
	// RegistryURL is the Harbor URL as configured, scheme included.
	RegistryURL string
	// RobotDuration is what the services put into RobotSpec.Duration, the
	// only lifetime the registry adapter reads.
	RobotDuration time.Duration
	TokenTTL      time.Duration
	// StaleThreshold is how long a satellite without an @every interval
	// may stay silent before it is stale (STALE_THRESHOLD); the floor of
	// every threshold is twice HeartbeatFlushInterval, because a beat
	// reaches the database only with the next flush.
	StaleThreshold, HeartbeatFlushInterval time.Duration
	// WebhooksEnabled is WEBHOOKS_ENABLED (plan item 9.1).
	WebhooksEnabled bool
}

// GroupStateRef is <registry>/satellite/group-state/<group>/state:latest.
// The state refs fail with ErrInvalid for a name that is not a valid
// resource name (utils.IsValidName).
func (s Settings) GroupStateRef(group string) (ArtifactRef, error) {
	return ArtifactRef{}, notimpl.Err("1.2 group state ref")
}

// ConfigStateRef is <registry>/satellite/config-state/<config>/state:latest.
func (s Settings) ConfigStateRef(config string) (ArtifactRef, error) {
	return ArtifactRef{}, notimpl.Err("1.2 config state ref")
}

// SatelliteStateRef is <registry>/satellite/satellite-state/<satellite>/state:latest.
func (s Settings) SatelliteStateRef(satellite string) (ArtifactRef, error) {
	return ArtifactRef{}, notimpl.Err("1.2 satellite state ref")
}

// RenderSatelliteState returns the satellite's state document, byte for
// byte what server.SatelliteStateArtifact encodes to, and its digest.
func RenderSatelliteState(state DesiredState) ([]byte, Digest, error) {
	return nil, "", notimpl.Err("1.2 render satellite state")
}

// RenderSatelliteDesiredState is the document the satellite reconciler
// records: {"robot_projects": [...], "state": <RenderSatelliteState>}, with
// "robot_disabled": true added only for a revoked satellite, so the digest
// of every other satellite stays what it was. Its digest covers every
// part, so a change of either is published; PushState stores only the
// state part.
func RenderSatelliteDesiredState(state DesiredState, robotProjects []string, robotDisabled bool) ([]byte, Digest, error) {
	return nil, "", notimpl.Err("2.3 render satellite desired state")
}

// RobotName is the registry robot name of a satellite created from plan
// item 2 on: unique per satellite ID, so a robot left over from a deleted
// satellite of the same name is never adopted. Robots that exist keep
// their names.
func RobotName(id SatelliteID, satellite string) string {
	return satellite + "-" + strconv.FormatInt(int64(id), 10)
}

// RenderGroupState returns the group's state document, byte for byte what
// server.GroupSyncRequest encodes to with Registry set, and its digest.
func RenderGroupState(group Group, registryURL string) ([]byte, Digest, error) {
	return nil, "", notimpl.Err("1.2 render group state")
}
