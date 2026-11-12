package fleettest

import (
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.Clock = Clock{}

// Clock is a fleet.Clock stopped at T.
type Clock struct {
	T time.Time
}

func (c Clock) Now() time.Time {
	return c.T
}
