package fleettest

import (
	"github.com/container-registry/harbor-satellite/internal/groundcontrol/fleet"
)

var _ fleet.RotationSource = (*RotationSource)(nil)

// RotationSource answers Pending from ByID; its calls are noted as
// "Rotations.Pending", because the real source runs no statement.
type RotationSource struct {
	*Recorder
	ByID map[fleet.SatelliteID]fleet.RotatedCredentials
}

func NewRotationSource(rec *Recorder) *RotationSource {
	return &RotationSource{Recorder: orNew(rec)}
}

func (r *RotationSource) Pending(satellite fleet.SatelliteID) (fleet.RotatedCredentials, bool) {
	r.Note("Rotations.Pending", satellite)
	c, ok := r.ByID[satellite]
	return c, ok
}
