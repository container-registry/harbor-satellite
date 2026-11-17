package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"
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

// validName is utils.IsValidName: 1 to 255 characters of lowercase
// letters, digits and ._-, starting with a letter or digit.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidName reports whether name is a valid satellite, group or config
// name.
func ValidName(name string) bool {
	return len(name) <= 255 && validName.MatchString(name)
}

// ReservedSatelliteNames and ReservedGroupNames are literal path segments
// that the API router matches before the {satellite} or {group} parameter
// of the same method: GET /api/satellites/active and /stale come before
// GET /api/satellites/{satellite}, DELETE /api/groups/satellite before
// DELETE /api/groups/{group}. A new satellite or group may not take them.
// ValidName still accepts them, so state refs of existing resources with
// such a name keep working.
var (
	ReservedSatelliteNames = []string{"active", "stale"}
	ReservedGroupNames     = []string{"satellite"}
)

func reservedName(kind, name string, reserved []string) error {
	if slices.Contains(reserved, name) {
		return fmt.Errorf("%w: %s name %q is reserved by the API", ErrInvalid, kind, name)
	}
	return nil
}

func (s Settings) stateRef(kind, name string) (ArtifactRef, error) {
	if !ValidName(name) {
		return ArtifactRef{}, fmt.Errorf("%w: %s name %q", ErrInvalid, kind, name)
	}
	return ArtifactRef{Registry: s.RegistryURL, Repository: SatelliteProject + "/" + kind + "-state/" + name + "/state", Tag: "latest"}, nil
}

// GroupStateRef is <registry>/satellite/group-state/<group>/state:latest.
// The state refs fail with ErrInvalid for a name that is not a valid
// resource name (utils.IsValidName).
func (s Settings) GroupStateRef(group string) (ArtifactRef, error) {
	return s.stateRef("group", group)
}

// ConfigStateRef is <registry>/satellite/config-state/<config>/state:latest.
func (s Settings) ConfigStateRef(config string) (ArtifactRef, error) {
	return s.stateRef("config", config)
}

// SatelliteStateRef is <registry>/satellite/satellite-state/<satellite>/state:latest.
func (s Settings) SatelliteStateRef(satellite string) (ArtifactRef, error) {
	return s.stateRef("satellite", satellite)
}

func (s Settings) tokenTTL() time.Duration {
	if s.TokenTTL > 0 {
		return s.TokenTTL
	}
	return DefaultTokenTTL
}

func digestOf(doc []byte) Digest {
	sum := sha256.Sum256(doc)
	return Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// satelliteStateDoc has server.SatelliteStateArtifact's field order and
// tags, so both encode to the same bytes.
type satelliteStateDoc struct {
	Config string   `json:"config,omitempty,omitzero"`
	States []string `json:"states,omitempty,omitzero"`
}

// RenderSatelliteState returns the satellite's state document, byte for
// byte what server.SatelliteStateArtifact encodes to, and its digest.
func RenderSatelliteState(state DesiredState) ([]byte, Digest, error) {
	doc := satelliteStateDoc{Config: state.ConfigState.String()}
	if state.ConfigState == (ArtifactRef{}) {
		doc.Config = ""
	}
	for _, g := range state.GroupStates {
		doc.States = append(doc.States, g.String())
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, "", fmt.Errorf("render satellite state: %w", err)
	}
	return data, digestOf(data), nil
}

type satelliteDesiredDoc struct {
	RobotDisabled bool            `json:"robot_disabled,omitempty"`
	RobotProjects []string        `json:"robot_projects"`
	State         json.RawMessage `json:"state"`
}

// RenderSatelliteDesiredState is the document the satellite reconciler
// records: {"robot_projects": [...], "state": <RenderSatelliteState>}, with
// "robot_disabled": true added only for a revoked satellite, so the digest
// of every other satellite stays what it was. Its digest covers every
// part, so a change of either is published; PushState stores only the
// state part.
func RenderSatelliteDesiredState(state DesiredState, robotProjects []string, robotDisabled bool) ([]byte, Digest, error) {
	stateDoc, _, err := RenderSatelliteState(state)
	if err != nil {
		return nil, "", err
	}
	if robotProjects == nil {
		robotProjects = []string{}
	}
	data, err := json.Marshal(satelliteDesiredDoc{RobotDisabled: robotDisabled, RobotProjects: robotProjects, State: stateDoc})
	if err != nil {
		return nil, "", fmt.Errorf("render satellite desired state: %w", err)
	}
	return data, digestOf(data), nil
}

// SatelliteStatePart returns the state document inside a document of
// RenderSatelliteDesiredState, which is what PushState stores.
func SatelliteStatePart(desired []byte) (json.RawMessage, []string, bool, error) {
	var doc satelliteDesiredDoc
	if err := json.Unmarshal(desired, &doc); err != nil {
		return nil, nil, false, fmt.Errorf("decode satellite desired state: %w", err)
	}
	return doc.State, doc.RobotProjects, doc.RobotDisabled, nil
}

// RobotName is the registry robot name of a satellite created from plan
// item 2 on: unique per satellite ID, so a robot left over from a deleted
// satellite of the same name is never adopted. Robots that exist keep
// their names.
func RobotName(id SatelliteID, satellite string) string {
	return satellite + "-" + strconv.FormatInt(int64(id), 10)
}

// groupStateDoc has server.GroupSyncRequest's field order and tags.
type groupStateDoc struct {
	Artifacts []Artifact `json:"artifacts,omitempty,omitzero"`
	Group     string     `json:"group"`
	Registry  string     `json:"registry,omitempty,omitzero"`
}

// RenderGroupState returns the group's state document, byte for byte what
// server.GroupSyncRequest encodes to with Registry set, and its digest.
func RenderGroupState(group Group, registryURL string) ([]byte, Digest, error) {
	data, err := json.Marshal(groupStateDoc{Artifacts: group.Artifacts, Group: group.Name, Registry: registryURL})
	if err != nil {
		return nil, "", fmt.Errorf("render group state: %w", err)
	}
	return data, digestOf(data), nil
}
