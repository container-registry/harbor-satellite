package fleet

import (
	"encoding/json"
	"time"
)

type (
	SatelliteID int32
	GroupID     int32
	ConfigID    int32
	// RobotID is Ground Control's row ID of a robot account; Harbor's own
	// robot ID is Robot.HarborID.
	RobotID int32
	// Digest is "sha256:<hex>".
	Digest string
)

// SatelliteProject is the registry project holding every state artifact.
// Every satellite robot may pull from it.
const SatelliteProject = "satellite"

type Satellite struct {
	ID   SatelliteID
	Name string
	// SPIFFEID is set for satellites registered through SPIRE; it goes into
	// the tombstone so the SPIRE entries are deleted with the satellite.
	SPIFFEID             string
	CreatedAt, UpdatedAt time.Time
	LastSeen             time.Time
	HeartbeatInterval    string
}

// Artifact is one entry of a group's state. Its JSON equals the generated
// server.GroupArtifact byte for byte, field order included, so a group
// state rendered from the domain keeps today's digest.
type Artifact struct {
	Deleted    bool     `json:"deleted,omitempty,omitzero"`
	Digest     string   `json:"digest,omitempty,omitzero"`
	Labels     any      `json:"labels,omitempty,omitzero"`
	Repository string   `json:"repository,omitempty,omitzero"`
	Tags       []string `json:"tag,omitempty,omitzero"`
	Type       string   `json:"type,omitempty,omitzero"`
}

type Group struct {
	ID                   GroupID
	Name, RegistryURL    string
	Projects             []string
	Artifacts            []Artifact
	CreatedAt, UpdatedAt time.Time
}

type Config struct {
	ID                ConfigID
	Name, RegistryURL string
	Document          json.RawMessage
	// Version is the optimistic-concurrency version of plan item 3.4.
	Version              int32
	CreatedAt, UpdatedAt time.Time
}

// ArtifactRef names a state artifact. Registry is the registry URL as
// configured, scheme included, because satellites receive state URLs in
// that form today.
type ArtifactRef struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

func (r ArtifactRef) String() string {
	return r.Registry + "/" + r.Repository + ":" + r.Tag
}

// DesiredState is what a satellite should run. With no GroupStates it is
// still rendered and published (D10).
type DesiredState struct {
	Satellite   SatelliteID
	GroupStates []ArtifactRef
	ConfigState ArtifactRef
}

type Robot struct {
	ID         RobotID
	Satellite  SatelliteID
	Name       string
	HarborID   int64
	SecretHash string
	ExpiresAt  time.Time
}

// RobotGrant is what one satellite's robot may pull: Projects are the
// projects of all its groups as stored, before RobotProjects.
type RobotGrant struct {
	Satellite SatelliteID
	HarborID  int64
	RobotName string
	Projects  []string
}

// RobotProjects is the rule for a robot's pull permissions: the projects
// of all of the satellite's groups, de-duplicated and sorted, plus
// SatelliteProject for the state artifacts.
func RobotProjects(projects []string) []string {
	return nil
}

type RobotSpec struct {
	Name     string
	Projects []string
	Duration time.Duration
}

type RobotCredentials struct {
	HarborID     int64
	Name, Secret string
	ExpiresAt    time.Time
}

// Agent is a SPIRE agent as the workload identity provider lists it.
type Agent struct {
	SPIFFEID, AttestationType string
	Selectors                 []string
	ExpiresAt                 time.Time
}
