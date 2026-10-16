package fleet

import (
	"strconv"
	"strings"
	"time"
)

// EventRobotRotated is appended when a successor robot is promoted (plan
// item 8.4); the satellite reconciler re-grants the successor's projects.
const EventRobotRotated EventKind = "RobotRotated"

type RobotRotatedPayload struct {
	Name       string `json:"name"`
	Generation int64  `json:"generation"`
	// RobotHarborIDs are the old and the new robot.
	RobotHarborIDs []int64 `json:"robot_harbor_ids"`
}

// RotatedCredentials are a successor robot's credentials as a heartbeat
// delivers them.
type RotatedCredentials struct {
	Username, Password string
	ExpiresAt          time.Time
	Generation         int64
}

// RotationSource answers from a per-replica cache, never with a statement.
type RotationSource interface {
	Pending(satellite SatelliteID) (RotatedCredentials, bool)
}

type RobotRotation struct {
	Satellite  SatelliteID
	Generation int64
	Successor  RobotCredentials
	Previous   Robot
}

// Sealer encrypts the successor's secret while it waits for the ack.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(ciphertext []byte) ([]byte, error)
}

// maxSuccessorName keeps Harbor's "robot$" prefix plus the name within
// robot_accounts.robot_name, VARCHAR(255).
const maxSuccessorName = 255 - len("robot$")

// SuccessorRobotName is unique per satellite ID and generation, so it never
// collides with another satellite's robot. A long satellite name is cut to
// fit maxSuccessorName; the ID keeps the name unique.
func SuccessorRobotName(id SatelliteID, satellite string, generation int64) string {
	suffix := "-" + strconv.FormatInt(int64(id), 10) + "-g" + strconv.FormatInt(generation, 10)
	if len(satellite)+len(suffix) > maxSuccessorName {
		satellite = strings.ToValidUTF8(satellite[:maxSuccessorName-len(suffix)], "")
	}
	return satellite + suffix
}
